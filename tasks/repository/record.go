package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	json "github.com/goccy/go-json"

	serviceerrors "github.com/elum2b/services/errors"
	callbackutil "github.com/elum2b/services/internal/utils/callback"
	tasksqlc "github.com/elum2b/services/tasks/sqlc"
	"github.com/jackc/pgx/v5/pgconn"
)

var ErrRecordAmountOverflow = serviceerrors.New(
	serviceerrors.CodeInvalidFields,
	"tasks record amount exceeds BIGINT",
)

var (
	ErrOperationIDRequired = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"tasks operation id is required",
	)
	ErrOperationIDInvalid = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"tasks operation id is invalid",
	)
	ErrOperationIDConflict = serviceerrors.New(
		serviceerrors.CodeConflict,
		"tasks operation id is already used",
	)
)

func (r *Repository) Record(ctx context.Context, params RecordParams) (RecordResult, error) {
	if err := params.Identity.Validate(); err != nil {
		return RecordResult{}, err
	}

	now := params.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	amount := params.Amount
	if amount == 0 {
		amount = 1
	}
	if amount > math.MaxInt64 {
		return RecordResult{}, ErrRecordAmountOverflow
	}
	for attempt := 0; attempt < 3; attempt++ {
		result := RecordResult{Status: RecordStatusNoTasks, Remaining: amount}
		err := r.recordInTx(ctx, params, now, amount, &result)
		if errors.Is(err, errRecordDuplicateEvent) {
			return result, nil
		}
		if isRetryableTxError(err) && attempt < 2 {
			continue
		}
		return result, err
	}
	return RecordResult{Status: RecordStatusNoTasks, Remaining: amount}, nil
}

var errRecordDuplicateEvent = errors.New("tasks: duplicate record event")

func (r *Repository) sequenceStatesForUser(ctx context.Context, identity Identity) (map[string]uint64, error) {
	return repositoryValue[map[string]uint64](ctx, r, func(ctx context.Context) (map[string]uint64, error) {
		rows, err := r.q.ListSequenceStatesForUser(ctx, tasksqlc.ListSequenceStatesForUserParams{
			WorkspaceID: identity.WorkspaceID,
			AppID:       identity.AppID, PlatformID: identity.PlatformID, PlatformUserID: identity.PlatformUserID,
		})
		if err != nil {
			return nil, err
		}
		result := make(map[string]uint64, len(rows))
		for _, row := range rows {
			if row.CurrentTaskID.Valid {
				result[row.SequenceKey] = uint64(row.CurrentTaskID.Int64)
			} else {
				result[row.SequenceKey] = 0
			}
		}
		return result, nil
	})
}

func (r *Repository) currentProgressForUpdate(
	ctx context.Context,
	identity Identity,
	taskID uint64,
	now time.Time,
) (Progress, error) {
	return repositoryValue[Progress](ctx, r, func(ctx context.Context) (Progress, error) {
		row, err := r.q.GetCurrentProgressForUpdate(ctx, tasksqlc.GetCurrentProgressForUpdateParams{
			WorkspaceID: identity.WorkspaceID, TaskID: int64(taskID),
			AppID: identity.AppID, PlatformID: identity.PlatformID, PlatformUserID: identity.PlatformUserID,
			PeriodStartAt: now, PeriodEndAt: now,
		})
		if err != nil {
			return Progress{}, err
		}
		return mapProgress(row), nil
	})
}

func (r *Repository) recordInTx(
	ctx context.Context,
	params RecordParams,
	now time.Time,
	amount uint64,
	result *RecordResult,
) error {
	return r.WithTx(ctx, func(txRepo *Repository) error {
		if err := txRepo.lockTaskUser(ctx, params.Identity); err != nil {
			return err
		}

		catalog, err := txRepo.listRecordCatalog(ctx, params.Identity.WorkspaceID, params.ActionKey)
		if err != nil {
			return err
		}
		if len(catalog) == 0 {
			return nil
		}
		var sequenceStates map[string]uint64
		if catalogHasSequenceTasks(catalog) {
			var err error
			sequenceStates, err = txRepo.sequenceStatesForUser(ctx, params.Identity)
			if err != nil {
				return err
			}
		}
		taskIDs := make([]int64, 0, len(catalog))
		for _, task := range catalog {
			taskIDs = append(taskIDs, int64(task.ID))
		}

		progressRows, err := repositoryValue[[]tasksqlc.TaskProgress](
			ctx,
			txRepo,
			func(ctx context.Context) ([]tasksqlc.TaskProgress, error) {
				return txRepo.q.ListCurrentProgressForTasksForUpdate(
					ctx,
					tasksqlc.ListCurrentProgressForTasksForUpdateParams{
						WorkspaceID:    params.Identity.WorkspaceID,
						AppID:          params.Identity.AppID,
						PlatformID:     params.Identity.PlatformID,
						PlatformUserID: params.Identity.PlatformUserID,
						PeriodStartAt:  now, PeriodEndAt: now,
						TaskIds: taskIDs,
					},
				)
			},
		)
		if err != nil {
			return err
		}
		progressByTask := make(map[uint64]Progress, len(progressRows))
		for _, row := range progressRows {
			progressByTask[uint64(row.TaskID)] = mapProgress(row)
		}
		changedTaskIDs := make([]uint64, 0, len(catalog))
		branches := make(map[string]struct{})
		progressUpserts := make([]recordProgressUpsert, 0, len(catalog))
		autoClaims := make([]recordAutoClaim, 0)
		shouldInsertEvent := false
		var totalConsumed uint64
		var maxConsumed uint64
		for _, task := range catalog {
			if !taskVisibleAt(task, now) {
				continue
			}
			branch := branchKey(task)
			if _, done := branches[branch]; done {
				continue
			}
			periodStart, periodEnd := periodFor(task, now)
			progress, exists := progressByTask[task.ID]
			if task.SequenceKey != nil {
				if exists && progress.Status == StatusClaimed {
					continue
				}
				currentTaskID, hasState := sequenceStates[*task.SequenceKey]
				if hasState {
					if currentTaskID != task.ID {
						continue
					}
				} else if task.SequencePosition == nil || *task.SequencePosition != 1 {
					continue
				}
				branches[branch] = struct{}{}
			} else if task.ActionKey != params.ActionKey {
				continue
			} else {
				branches[branch] = struct{}{}
			}
			if task.ActionKey != params.ActionKey {
				continue
			}
			if params.ExternalEventKey != "" {
				shouldInsertEvent = true
			}
			if progress.Status == StatusClaimed || progress.Status == StatusReady {
				continue
			}
			if task.StartMode == StartModeRequired && !exists {
				continue
			}
			before := progress.Progress
			need := task.TargetCount - progress.Progress
			consume := amount
			if consume > need {
				consume = need
			}
			progress.Progress += consume
			claimed := false
			if progress.Progress >= task.TargetCount {
				if task.ClaimMode == ClaimModeAuto {
					autoClaims = append(autoClaims, recordAutoClaim{
						task: task, progress: progress, exists: exists,
						periodStartAt: periodStart, periodEndAt: periodEnd,
					})
					claimed = true
				} else {
					progress.Status = StatusReady
					progress.ReadyAt = &now
					progressUpserts = append(progressUpserts, recordProgressUpsert{
						taskID:        task.ID,
						periodStartAt: periodStart,
						periodEndAt:   periodEnd,
						delta:         consume,
						status:        progress.Status,
						readyAt:       progress.ReadyAt,
						rewards:       task.Rewards,
					})
					changedTaskIDs = append(changedTaskIDs, task.ID)
				}
			} else {
				progressUpserts = append(progressUpserts, recordProgressUpsert{
					taskID:        task.ID,
					periodStartAt: periodStart,
					periodEndAt:   periodEnd,
					delta:         consume,
					status:        StatusOpen,
					rewards:       task.Rewards,
				})
			}
			result.Status = RecordStatusRecorded
			totalConsumed += consume
			if consume > maxConsumed {
				maxConsumed = consume
			}
			result.Tasks = append(result.Tasks, TaskResult{
				Task: task, Before: before, After: progress.Progress, Consumed: consume, Claimed: claimed,
			})
		}
		if shouldInsertEvent {
			eventPayload := params.Payload
			if len(eventPayload) == 0 {
				eventPayload = []byte("{}")
			}
			affected, err := repositoryValue[int64](ctx, txRepo, func(ctx context.Context) (int64, error) {
				return txRepo.q.InsertProgressEvent(ctx, tasksqlc.InsertProgressEventParams{
					WorkspaceID:      params.Identity.WorkspaceID,
					AppID:            params.Identity.AppID,
					PlatformID:       params.Identity.PlatformID,
					PlatformUserID:   params.Identity.PlatformUserID,
					Source:           params.Source,
					ExternalEventKey: params.ExternalEventKey,
					ActionKey:        params.ActionKey,
					Amount:           int64(amount),
					Payload:          rawMessageParam(eventPayload),
				})
			})
			if err != nil {
				return err
			}
			if affected != 1 {
				*result = RecordResult{Status: RecordStatusDuplicate, Remaining: amount}
				return errRecordDuplicateEvent
			}
		}
		if _, err := txRepo.batchUpsertProgress(ctx, params.Identity, progressUpserts); err != nil {
			return err
		}
		if err := txRepo.refreshComplexParentsForChangedTasks(ctx, params.Identity, changedTaskIDs, now); err != nil {
			return err
		}
		for _, item := range autoClaims {
			progress := item.progress
			if !item.exists {
				progress, err = txRepo.ensureProgress(
					ctx,
					params.Identity,
					item.task,
					item.periodStartAt,
					item.periodEndAt,
				)
				if err != nil {
					return err
				}
				progress.Progress = item.progress.Progress
			}
			if err := txRepo.claimProgress(ctx, params.Identity, &item.task, &progress, autoOperationID(params.ExternalEventKey, item.task.ID), now); err != nil {
				return err
			}
		}
		if len(result.Tasks) == 0 {
			return nil
		}
		result.Consumed = totalConsumed
		result.Remaining = amount - maxConsumed
		return nil
	})
}

func isRetryableTxError(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	switch pgErr.Code {
	case "40001", "40P01", "55P03":
		return true
	default:
		return false
	}
}

func branchKey(task Task) string {
	if task.SequenceKey != nil {
		return "sequence:" + *task.SequenceKey
	}
	return fmt.Sprintf("task:%d", task.ID)
}

func catalogHasSequenceTasks(catalog []Task) bool {
	for _, task := range catalog {
		if task.SequenceKey != nil {
			return true
		}
	}
	return false
}

func (r *Repository) Claim(ctx context.Context, params ClaimParams) (ClaimResult, error) {
	if err := params.Identity.Validate(); err != nil {
		return ClaimResult{}, err
	}

	operationID, err := validateOperationID(params.OperationID)
	if err != nil {
		return ClaimResult{}, err
	}
	params.OperationID = operationID

	now := params.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	result := ClaimResult{Status: ClaimStatusNotFound}
	err = r.WithTx(ctx, func(txRepo *Repository) error {
		if err := txRepo.lockTaskUser(ctx, params.Identity); err != nil {
			return err
		}

		id, key := taskRef(params.TaskRef)
		var task Task
		var err error
		if id != 0 {
			task, err = txRepo.claimCatalogByID(ctx, params.Identity.WorkspaceID, id)
		} else {
			task, err = txRepo.claimCatalogByKey(ctx, params.Identity.WorkspaceID, key)
		}
		if err != nil {
			if isNoRows(err) {
				return nil
			}
			return err
		}
		result.Task = &task
		refreshedProgress, err := txRepo.refreshComplexTaskBeforeClaim(ctx, params.Identity, task, now)
		if err != nil {
			return err
		}
		var progress Progress
		if refreshedProgress != nil && refreshedProgress.ID != 0 {
			progress = *refreshedProgress
		} else {
			progress, err = txRepo.currentProgressForUpdate(ctx, params.Identity, task.ID, now)
			if err != nil {
				if isNoRows(err) {
					if task.StartMode == StartModeRequired {
						result.Status = ClaimStatusNotStarted
					} else {
						result.Status = ClaimStatusNotReady
					}
					return nil
				}
				return err
			}
		}
		task.Progress = &progress
		result.Task = &task
		if task.Progress == nil {
			result.Status = ClaimStatusNotReady
			return nil
		}
		switch task.Progress.Status {
		case StatusClaimed:
			if task.Progress.OperationID == nil ||
				*task.Progress.OperationID != params.OperationID {
				return ErrOperationIDConflict
			}

			result.Status = ClaimStatusAlreadyDone
			return nil
		case StatusReady:
			if err := txRepo.claimProgress(ctx, params.Identity, &task, task.Progress, params.OperationID, now); err != nil {
				return err
			}
			result.Task = &task
			result.Status = ClaimStatusClaimed
			return nil
		default:
			result.Status = ClaimStatusNotReady
			return nil
		}
	})
	return result, err
}

func (r *Repository) StartTask(ctx context.Context, params StartTaskParams) (StartTaskResult, error) {
	if err := params.Identity.Validate(); err != nil {
		return StartTaskResult{}, err
	}

	now := params.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	result := StartTaskResult{Status: ClaimStatusNotFound}
	err := r.WithTx(ctx, func(txRepo *Repository) error {
		if err := txRepo.lockTaskUser(ctx, params.Identity); err != nil {
			return err
		}

		id, key := taskRef(params.TaskRef)
		var (
			task Task
			err  error
		)
		if id != 0 {
			row, err := txRepo.q.GetStartTaskByID(
				ctx,
				tasksqlc.GetStartTaskByIDParams{WorkspaceID: params.Identity.WorkspaceID, ID: int64(id)},
			)
			if err == nil {
				task = mapStartTaskByID(row)
			}
		} else {
			row, rowErr := txRepo.q.GetStartTaskByKey(ctx, tasksqlc.GetStartTaskByKeyParams{WorkspaceID: params.Identity.WorkspaceID, Key: key})
			err = rowErr
			if err == nil {
				task = mapStartTaskByKey(row)
			}
		}
		if err != nil {
			if isNoRows(err) {
				return nil
			}
			return err
		}
		result.Task = &task
		if !taskVisibleAt(task, now) {
			result.Status = RecordStatusNoTasks
			return nil
		}
		allowed, err := txRepo.integrationTaskSequenceReady(ctx, params.Identity, task)
		if err != nil {
			return err
		}
		if !allowed {
			result.Status = RecordStatusNoTasks
			return nil
		}
		periodStart, periodEnd := periodFor(task, now)
		progress, err := txRepo.currentProgressForUpdate(ctx, params.Identity, task.ID, now)
		if err != nil {
			if !isNoRows(err) {
				return err
			}

			task.Rewards, err = txRepo.rewards(ctx, task.WorkspaceID, task.ID)
			if err != nil {
				return err
			}

			progress, err = txRepo.ensureProgress(ctx, params.Identity, task, periodStart, periodEnd)
			if err != nil {
				return err
			}
		}
		task.Progress = &progress
		result.Task = &task
		if progress.Status == StatusClaimed || progress.Status == StatusReady {
			result.Status = progress.Status
			return nil
		}
		result.Status = StartStatusStarted
		return nil
	})
	return result, err
}

func (r *Repository) GetClaimTask(
	ctx context.Context,
	identity Identity,
	taskRefValue string,
	now time.Time,
) (Task, bool, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var result Task
	found := false
	err := r.WithTx(ctx, func(txRepo *Repository) error {
		if err := txRepo.lockTaskUser(ctx, identity); err != nil {
			return err
		}

		id, key := taskRef(taskRefValue)
		var (
			task Task
			err  error
		)
		if id != 0 {
			task, err = txRepo.claimCatalogByID(ctx, identity.WorkspaceID, id)
		} else {
			task, err = txRepo.claimCatalogByKey(ctx, identity.WorkspaceID, key)
		}
		if err != nil {
			if isNoRows(err) {
				return nil
			}
			return err
		}
		found = true
		progress, err := txRepo.currentProgressForUpdate(ctx, identity, task.ID, now)
		if err != nil {
			if !isNoRows(err) {
				return err
			}
		} else {
			task.Progress = &progress
		}
		result = task
		return nil
	})
	return result, found, err
}

func (r *Repository) ensureProgress(
	ctx context.Context,
	identity Identity,
	task Task,
	start, end time.Time,
) (Progress, error) {
	id, err := repositoryValue[int64](ctx, r, func(ctx context.Context) (int64, error) {
		return r.q.EnsureProgress(ctx, tasksqlc.EnsureProgressParams{
			WorkspaceID:    identity.WorkspaceID,
			TaskID:         int64(task.ID),
			AppID:          identity.AppID,
			PlatformID:     identity.PlatformID,
			PlatformUserID: identity.PlatformUserID,
			PeriodStartAt:  start,
			PeriodEndAt:    end,
			RewardsSnapshot: rawMessageParam(
				rewardsSnapshot(task.Rewards),
			),
		})
	})
	if err != nil {
		return Progress{}, err
	}
	return Progress{
		ID: uint64(id), Progress: 0, Status: StatusOpen,
		PeriodStartAt: start, PeriodEndAt: end, Rewards: task.Rewards,
	}, nil
}

func (r *Repository) saveProgress(ctx context.Context, progress Progress) error {
	_, err := repositoryValue[int64](ctx, r, func(ctx context.Context) (int64, error) {
		return r.q.UpdateProgress(ctx, tasksqlc.UpdateProgressParams{
			Progress: int64(progress.Progress), Status: progress.Status,
			ReadyAt: nullTime(progress.ReadyAt), ClaimedAt: nullTime(progress.ClaimedAt),
			OperationID: nullString(
				progress.OperationID,
			), RewardsSnapshot: rawMessageParam(rewardsSnapshot(progress.Rewards)),
			ID: int64(progress.ID),
		})
	})
	return err
}

func (r *Repository) claimProgress(
	ctx context.Context,
	identity Identity,
	task *Task,
	progress *Progress,
	operationID string,
	now time.Time,
) error {
	rewards := progress.Rewards
	if rewards == nil {
		rewards = task.Rewards
	}
	if rewards == nil {
		var err error
		rewards, err = r.rewards(ctx, task.WorkspaceID, task.ID)
		if err != nil {
			return err
		}
	}
	claimed, err := r.q.ClaimProgressWithOperation(ctx, tasksqlc.ClaimProgressWithOperationParams{
		WorkspaceID:     identity.WorkspaceID,
		OperationID:     operationID,
		ProgressID:      int64(progress.ID),
		Progress:        int64(progress.Progress),
		ReadyAt:         nullTime(progress.ReadyAt),
		ClaimedAt:       now,
		RewardsSnapshot: rewardsSnapshot(rewards),
	})
	if err != nil {
		return err
	}
	if claimed != 1 {
		return ErrOperationIDConflict
	}

	progress.Status = StatusClaimed
	progress.ClaimedAt = &now
	progress.OperationID = &operationID
	progress.Rewards = rewards
	if err := r.advanceSequenceState(ctx, identity, task); err != nil {
		return err
	}
	if err := r.refreshComplexParentsForChangedTasks(ctx, identity, []uint64{task.ID}, now); err != nil {
		return err
	}
	task.Rewards = rewards
	payload, err := json.Marshal(CallbackPayload{
		WorkspaceID: identity.WorkspaceID, AppID: identity.AppID, PlatformID: identity.PlatformID,
		PlatformUserID: identity.PlatformUserID, TaskID: task.ID, TaskKey: task.Key,
		OperationID: operationID, PeriodStartAt: progress.PeriodStartAt,
		PeriodEndAt: progress.PeriodEndAt, Rewards: rewards, Payload: task.Payload,
	})
	if err != nil {
		return err
	}
	eventKey := fmt.Sprintf("tasks.claimed:%d", progress.ID)
	_, err = repositoryValue[uint64](ctx, r, func(ctx context.Context) (uint64, error) {
		return r.callbacks.CreateEvent(ctx, callbackutil.CreateParams{
			WorkspaceID: identity.WorkspaceID, SourceService: "tasks", EventType: CallbackEventClaimed,
			EventKey: eventKey, IdempotencyKey: eventKey,
			Payload: payload, NextAttemptAt: now,
		})
	})
	return err
}

func validateOperationID(value string) (string, error) {

	value = strings.TrimSpace(value)
	if value == "" {
		return "", ErrOperationIDRequired
	}
	if len(value) > 128 {
		return "", ErrOperationIDInvalid
	}

	return value, nil

}

func (r *Repository) advanceSequenceState(ctx context.Context, identity Identity, task *Task) error {
	if task.SequenceKey == nil || task.SequencePosition == nil {
		return nil
	}
	next, err := r.nextSequenceTask(ctx, task.WorkspaceID, *task.SequenceKey, *task.SequencePosition)
	status := "active"
	currentTaskID := sql.NullInt64{}
	if err != nil {
		return err
	}
	if !next.Exists {
		status = "completed"
	} else {
		currentTaskID = sql.NullInt64{Int64: int64(next.ID), Valid: true}
	}
	return repositoryExec(ctx, r, func(ctx context.Context) error {
		return r.q.UpsertSequenceState(ctx, tasksqlc.UpsertSequenceStateParams{
			WorkspaceID: task.WorkspaceID, SequenceKey: *task.SequenceKey,
			AppID: identity.AppID, PlatformID: identity.PlatformID, PlatformUserID: identity.PlatformUserID,
			CurrentTaskID: currentTaskID, Status: status,
		})
	})
}

func (r *Repository) rewards(ctx context.Context, workspaceID string, taskID uint64) ([]Reward, error) {
	return r.rewardsCatalog(ctx, workspaceID, taskID)
}

func rewardsSnapshot(rewards []Reward) json.RawMessage {
	if rewards == nil {
		return nil
	}
	raw, _ := json.Marshal(rewards)
	return raw
}

func autoOperationID(eventKey string, taskID uint64) string {
	if eventKey != "" {
		digest := sha256.Sum256([]byte(eventKey))

		return fmt.Sprintf("auto:%d:%s", taskID, hex.EncodeToString(digest[:16]))
	}
	return fmt.Sprintf("auto-%d-%d", taskID, time.Now().UnixNano())
}
