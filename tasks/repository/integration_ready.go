package repository

import (
	"context"
	"time"

	tasksqlc "github.com/elum2b/services/tasks/sqlc"
)

func (r *Repository) MarkIntegrationTaskReady(
	ctx context.Context,
	params MarkIntegrationTaskReadyParams,
) (MarkIntegrationTaskReadyResult, error) {
	now := params.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	task := params.Task
	result := MarkIntegrationTaskReadyResult{Status: RecordStatusNoTasks, Task: task}
	if !taskVisibleAt(task, now) {
		return result, nil
	}
	for attempt := 0; attempt < 3; attempt++ {
		err := r.WithTx(ctx, func(txRepo *Repository) error {
			if err := txRepo.lockTaskUser(ctx, params.Identity); err != nil {
				return err
			}

			allowed, err := txRepo.integrationTaskSequenceReady(ctx, params.Identity, task)
			if err != nil {
				return err
			}
			if !allowed {
				result.Status = RecordStatusNoTasks
				return nil
			}
			progress, exists, err := txRepo.integrationTaskProgressForUpdate(ctx, params.Identity, task.ID, now)
			if err != nil {
				return err
			}
			if progress.Status == StatusClaimed || progress.Status == StatusReady {
				task.Progress = &progress
				result.Task = task
				result.Status = RecordStatusDuplicate
				return nil
			}
			if task.StartMode == StartModeRequired && !exists {
				result.Status = ClaimStatusNotStarted
				return nil
			}
			if params.ExternalEventKey != "" {
				inserted, err := txRepo.insertProgressEvent(ctx, params)
				if err != nil {
					return err
				}
				if !inserted {
					result.Status = RecordStatusDuplicate
					return nil
				}
			}
			periodStart, periodEnd := periodFor(task, now)
			if !exists {
				task.Rewards, err = txRepo.rewards(ctx, task.WorkspaceID, task.ID)
				if err != nil {
					return err
				}
			}

			if exists {
				progress.Progress = task.TargetCount
				progress.Status = StatusReady
				progress.ReadyAt = &now
				if err := txRepo.saveProgress(ctx, progress); err != nil {
					return err
				}
			} else if _, err := txRepo.batchUpsertProgress(ctx, params.Identity, []recordProgressUpsert{{
				taskID:        task.ID,
				periodStartAt: periodStart,
				periodEndAt:   periodEnd,
				delta:         task.TargetCount,
				status:        StatusReady,
				readyAt:       &now,
				rewards:       task.Rewards,
			}}); err != nil {
				return err
			}
			progress.Progress = task.TargetCount
			progress.Status = StatusReady
			progress.ReadyAt = &now
			progress.PeriodStartAt = periodStart
			progress.PeriodEndAt = periodEnd
			task.Progress = &progress
			result.Task = task
			result.Status = RecordStatusRecorded
			if err := txRepo.refreshComplexParentsForChangedTasks(ctx, params.Identity, []uint64{task.ID}, now); err != nil {
				return err
			}
			return nil
		})
		if isRetryableTxError(err) && attempt < 2 {
			continue
		}
		return result, err
	}
	return result, nil
}

func (r *Repository) integrationTaskSequenceReady(ctx context.Context, identity Identity, task Task) (bool, error) {
	if task.SequenceKey == nil {
		return true, nil
	}
	row, err := r.q.GetSequenceState(ctx, tasksqlc.GetSequenceStateParams{
		WorkspaceID:    identity.WorkspaceID,
		SequenceKey:    *task.SequenceKey,
		AppID:          identity.AppID,
		PlatformID:     identity.PlatformID,
		PlatformUserID: identity.PlatformUserID,
	})
	if err != nil {
		if isNoRows(err) {
			return task.SequencePosition != nil && *task.SequencePosition == 1, nil
		}
		return false, err
	}
	return string(row.Status) == "active" && row.CurrentTaskID.Valid && uint64(row.CurrentTaskID.Int64) == task.ID, nil
}

func (r *Repository) lockTaskUser(ctx context.Context, identity Identity) error {
	return r.q.LockTaskUser(ctx, tasksqlc.LockTaskUserParams{
		WorkspaceID:    identity.WorkspaceID,
		AppID:          identity.AppID,
		PlatformID:     identity.PlatformID,
		PlatformUserID: identity.PlatformUserID,
	})
}

func (r *Repository) integrationTaskProgressForUpdate(
	ctx context.Context,
	identity Identity,
	taskID uint64,
	now time.Time,
) (Progress, bool, error) {
	progress, err := r.currentProgressForUpdate(ctx, identity, taskID, now)
	if err != nil {
		if isNoRows(err) {
			return Progress{}, false, nil
		}
		return Progress{}, false, err
	}
	return progress, true, nil
}

func (r *Repository) insertProgressEvent(ctx context.Context, params MarkIntegrationTaskReadyParams) (bool, error) {
	eventPayload := params.Payload
	if len(eventPayload) == 0 {
		eventPayload = []byte("{}")
	}
	affected, err := repositoryValue[int64](ctx, r, func(ctx context.Context) (int64, error) {
		return r.q.InsertProgressEvent(ctx, tasksqlc.InsertProgressEventParams{
			WorkspaceID:      params.Identity.WorkspaceID,
			AppID:            params.Identity.AppID,
			PlatformID:       params.Identity.PlatformID,
			PlatformUserID:   params.Identity.PlatformUserID,
			Source:           params.Source,
			ExternalEventKey: params.ExternalEventKey,
			ActionKey:        params.Task.ActionKey,
			Amount:           int64(params.Task.TargetCount),
			Payload:          rawMessageParam(eventPayload),
		})
	})
	if err != nil {
		return false, err
	}
	return affected == 1, nil
}
