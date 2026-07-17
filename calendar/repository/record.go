package repository

import (
	"context"
	"database/sql"
	"fmt"
	json "github.com/goccy/go-json"
	"time"

	calendarsqlc "github.com/elum2b/services/calendar/sqlc"
	callbackutil "github.com/elum2b/services/internal/utils/callback"
)

func (r *Repository) Record(ctx context.Context, params RecordParams) (RecordResult, error) {
	if err := params.Identity.Validate(); err != nil {
		return RecordResult{}, err
	}

	now := params.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var result RecordResult
	err := r.WithTx(ctx, func(txRepo *Repository) error {
		refID, calendarType := calendarReference(params.CalendarRef)
		rows, err := txRepo.q.GetRecordBundleForUpdate(ctx, calendarsqlc.GetRecordBundleForUpdateParams{
			AppID: params.Identity.AppID, PlatformID: params.Identity.PlatformID,
			PlatformUserID: params.Identity.PlatformUserID,
			AppID_2:        params.Identity.AppID, PlatformID_2: params.Identity.PlatformID,
			PlatformUserID_2: params.Identity.PlatformUserID, OperationID: params.OperationID,
			WorkspaceID: params.Identity.WorkspaceID, ID: refID, Type: calendarType,
		})
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			result = RecordResult{
				OperationID: params.OperationID, Status: StatusNotFound, OccurredAt: now,
			}
			return nil
		}
		if err := txRepo.q.EnsureProgressForUpdate(ctx, calendarsqlc.EnsureProgressForUpdateParams{
			WorkspaceID:    params.Identity.WorkspaceID,
			CalendarID:     rows[0].ID,
			AppID:          params.Identity.AppID,
			PlatformID:     params.Identity.PlatformID,
			PlatformUserID: params.Identity.PlatformUserID,
		}); err != nil {
			return err
		}
		if _, err := txRepo.q.LockProgressForUpdate(ctx, calendarsqlc.LockProgressForUpdateParams{
			WorkspaceID:    params.Identity.WorkspaceID,
			CalendarID:     rows[0].ID,
			AppID:          params.Identity.AppID,
			PlatformID:     params.Identity.PlatformID,
			PlatformUserID: params.Identity.PlatformUserID,
		}); err != nil {
			return err
		}
		rows, err = txRepo.q.GetRecordBundleForUpdate(ctx, calendarsqlc.GetRecordBundleForUpdateParams{
			AppID:            params.Identity.AppID,
			PlatformID:       params.Identity.PlatformID,
			PlatformUserID:   params.Identity.PlatformUserID,
			AppID_2:          params.Identity.AppID,
			PlatformID_2:     params.Identity.PlatformID,
			PlatformUserID_2: params.Identity.PlatformUserID,
			OperationID:      params.OperationID,
			WorkspaceID:      params.Identity.WorkspaceID,
			ID:               refID,
			Type:             calendarType,
		})
		if err != nil {
			return err
		}
		calendar, progress, repeated, err := mapRecordBundle(rows)
		if err != nil {
			return err
		}
		if repeated != nil {
			result = *repeated
			result.Calendar = calendar
			return nil
		}
		result, err = calculateRecord(calendar, progress, params.OperationID, now)
		if err != nil {
			return err
		}
		rawRewards, err := json.Marshal(result.Rewards)
		if err != nil {
			return err
		}
		id, err := txRepo.q.CreateOperation(ctx, calendarsqlc.CreateOperationParams{
			WorkspaceID: params.Identity.WorkspaceID, CalendarID: calendar.ID,
			AppID: params.Identity.AppID, PlatformID: params.Identity.PlatformID,
			PlatformUserID: params.Identity.PlatformUserID, OperationID: params.OperationID,
			Granted: result.Granted, Status: result.Status,
			Position: nullableUint32(result.Position), RewardsSnapshot: rawRewards,
			CurrentPosition: int32(result.Progress.CurrentPosition), ClaimCount: int64(result.Progress.ClaimCount),
			LastClaimPosition: nullableUint32(result.Progress.LastClaimPosition),
			LastClaimAt:       nullableTime(result.Progress.LastClaimAt),
			NextClaimAt:       nullableTime(result.Progress.NextClaimAt),
			IsCompleted:       result.Progress.IsCompleted, ResetCount: int64(result.Progress.ResetCount),
			WasReset: result.Progress.LastWasReset, OccurredAt: now,
		})
		if err != nil {
			return err
		}
		result.OperationRowID = uint64(id)
		if result.Granted {
			if err := txRepo.q.UpsertProgress(ctx, calendarsqlc.UpsertProgressParams{
				WorkspaceID:       params.Identity.WorkspaceID,
				CalendarID:        calendar.ID,
				AppID:             params.Identity.AppID,
				PlatformID:        params.Identity.PlatformID,
				PlatformUserID:    params.Identity.PlatformUserID,
				CurrentPosition:   int32(result.Progress.CurrentPosition),
				ClaimCount:        int64(result.Progress.ClaimCount),
				LastClaimPosition: nullableUint32(result.Progress.LastClaimPosition),
				LastClaimAt:       nullableTime(result.Progress.LastClaimAt),
				NextClaimAt:       nullableTime(result.Progress.NextClaimAt),
				IsCompleted:       result.Progress.IsCompleted,
				ResetCount:        int64(result.Progress.ResetCount),
				LastWasReset:      result.Progress.LastWasReset,
			}); err != nil {
				return err
			}
			payload, err := json.Marshal(rewardGrantedCallbackPayload{
				OperationRowID: result.OperationRowID,
				OperationID:    result.OperationID,
				WorkspaceID:    params.Identity.WorkspaceID,
				CalendarID:     calendar.ID,
				AppID:          params.Identity.AppID,
				PlatformID:     params.Identity.PlatformID,
				PlatformUserID: params.Identity.PlatformUserID,
				Position:       uint32Value(result.Position),
				Rewards:        result.Rewards,
				OccurredAt:     result.OccurredAt,
			})
			if err != nil {
				return err
			}
			eventKey := fmt.Sprintf("calendar.reward_granted:%d", result.OperationRowID)
			if _, err := txRepo.callbacks.CreateEvent(ctx, callbackutil.CreateParams{
				WorkspaceID:        params.Identity.WorkspaceID,
				SourceService:      "calendar",
				EventType:          "calendar.reward_granted",
				EventKey:           eventKey,
				IdempotencyKey:     eventKey,
				Payload:            payload,
				PayloadContentType: callbackutil.JSONContentType,
				NextAttemptAt:      now,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	return result, err
}

func mapRecordBundle(rows []calendarsqlc.GetRecordBundleForUpdateRow) (Calendar, Progress, *RecordResult, error) {
	row := rows[0]
	calendar := Calendar{
		ID: row.ID, WorkspaceID: row.WorkspaceID, Type: row.Type,
		Mode:                row.Mode,
		IntervalType:        row.IntervalType,
		IntervalUnit:        row.IntervalUnit,
		IntervalCount:       uint32(row.IntervalCount),
		ResetAfterIntervals: uint32(row.ResetAfterIntervals),
		EndBehavior:         row.EndBehavior,
		Timezone:            row.Timezone, HideFutureRewards: row.HideFutureRewards,
		IsActive: row.IsActive, StartAt: sqlNullTimePtr(row.StartAt),
		EndAt: sqlNullTimePtr(row.EndAt), DeletedAt: sqlNullTimePtr(row.DeletedAt),
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		Steps: make([]Step, 0),
	}
	for _, item := range rows {
		calendar.Steps = appendStep(
			calendar.Steps,
			item.StepID,
			item.StepPosition,
			item.RewardID,
			item.RewardItemKey,
			item.RewardType,
			item.RewardItemCount,
			item.RewardScale,
			item.RewardDurationUnit,
		)
	}
	progress := Progress{
		CurrentPosition:   uint32(row.CurrentPosition.Int32),
		ClaimCount:        uint64(row.ClaimCount.Int64),
		LastClaimPosition: sqlNullUint32Ptr(row.LastClaimPosition),
		LastClaimAt:       sqlNullTimePtr(row.LastClaimAt), NextClaimAt: sqlNullTimePtr(row.NextClaimAt),
		IsCompleted: row.IsCompleted.Bool, ResetCount: uint64(row.ResetCount.Int64),
		LastWasReset: row.LastWasReset.Bool,
	}
	if !row.OperationRowID.Valid {
		return calendar, progress, nil, nil
	}
	rewards, err := decodeRewards(row.OperationRewardsSnapshot)
	if err != nil {
		return Calendar{}, Progress{}, nil, err
	}
	repeated := &RecordResult{
		OperationRowID: uint64(row.OperationRowID.Int64),
		OperationID:    row.ExistingOperationID.String,
		Granted:        row.OperationGranted.Bool, Status: row.OperationStatus.String,
		Position: sqlNullUint32Ptr(row.OperationPosition), Rewards: rewards,
		Progress: Progress{
			CurrentPosition:   uint32(row.OperationCurrentPosition.Int32),
			ClaimCount:        uint64(row.OperationClaimCount.Int64),
			LastClaimPosition: sqlNullUint32Ptr(row.OperationLastClaimPosition),
			LastClaimAt:       sqlNullTimePtr(row.OperationLastClaimAt),
			NextClaimAt:       sqlNullTimePtr(row.OperationNextClaimAt),
			IsCompleted:       row.OperationIsCompleted.Bool,
			ResetCount:        uint64(row.OperationResetCount.Int64),
			LastWasReset:      row.OperationWasReset.Bool,
		},
		OccurredAt: row.OperationOccurredAt.Time,
	}
	return calendar, progress, repeated, nil
}

type rewardGrantedCallbackPayload struct {
	OperationRowID uint64    `json:"operation_row_id"`
	OperationID    string    `json:"operation_id"`
	WorkspaceID    string    `json:"workspace_id"`
	CalendarID     string    `json:"calendar_id"`
	AppID          int64     `json:"app_id"`
	PlatformID     int64     `json:"platform_id"`
	PlatformUserID string    `json:"platform_user_id"`
	Position       uint32    `json:"position"`
	Rewards        []Reward  `json:"rewards"`
	OccurredAt     time.Time `json:"occurred_at"`
}

func uint32Value(value *uint32) uint32 {
	if value == nil {
		return 0
	}
	return *value
}

func calculateRecord(calendar Calendar, progress Progress, operationID string, now time.Time) (RecordResult, error) {
	result := RecordResult{
		OperationID: operationID, Calendar: calendar, Status: StatusNotAvailable,
		Progress: progress, OccurredAt: now, Rewards: make([]Reward, 0),
	}
	switch {
	case calendar.DeletedAt != nil:
		result.Status = StatusNotFound
		return result, nil
	case !calendar.IsActive:
		result.Status = StatusInactive
		return result, nil
	case calendar.StartAt != nil && now.Before(*calendar.StartAt):
		result.Status = StatusNotStarted
		result.Progress.NextClaimAt = calendar.StartAt
		return result, nil
	case calendar.EndAt != nil && !now.Before(*calendar.EndAt):
		result.Status = StatusExpired
		return result, nil
	case len(calendar.Steps) == 0:
		result.Status = StatusNoSteps
		return result, nil
	}

	position, nextAt, reset, status, err := calculatePosition(calendar, progress, now)
	if err != nil {
		return RecordResult{}, err
	}
	if status != "" {
		result.Status = status
		result.Progress.NextClaimAt = nextAt
		return result, nil
	}
	step, found := stepAt(calendar.Steps, position)
	if !found {
		result.Status = StatusCompleted
		result.Progress.IsCompleted = true
		return result, nil
	}
	result.Granted = true
	result.Status = StatusGranted
	result.Position = &position
	result.Rewards = append(result.Rewards, step.Rewards...)
	result.Progress.CurrentPosition = position
	result.Progress.ClaimCount++
	result.Progress.LastClaimPosition = &position
	result.Progress.LastClaimAt = &now
	result.Progress.NextClaimAt = nextAt
	result.Progress.LastWasReset = reset
	if reset {
		result.Progress.ResetCount++
	}
	if position == calendar.Steps[len(calendar.Steps)-1].Position && calendar.EndBehavior == EndStop {
		result.Progress.IsCompleted = true
	}
	return result, nil
}

func calculatePosition(calendar Calendar, progress Progress, now time.Time) (uint32, *time.Time, bool, string, error) {
	if calendar.Mode == ModeInterval {
		if progress.NextClaimAt != nil && now.Before(*progress.NextClaimAt) {
			return 0, progress.NextClaimAt, false, StatusNotAvailable, nil
		}
		index, next, err := intervalIndex(calendar, now)
		if err != nil {
			return 0, nil, false, "", err
		}
		position, status := positionAtOrdinal(index, calendar)
		return position, &next, false, status, nil
	}
	if progress.IsCompleted && calendar.EndBehavior == EndStop {
		return 0, progress.NextClaimAt, false, StatusCompleted, nil
	}
	if progress.LastClaimAt != nil {
		next, err := nextAvailableAt(calendar, *progress.LastClaimAt)
		if err != nil {
			return 0, nil, false, "", err
		}
		if now.Before(next) {
			return 0, &next, false, StatusNotAvailable, nil
		}
		reset := false
		position, status := nextStepPosition(progress.CurrentPosition, calendar)
		if calendar.Mode == ModeSequentialReset {
			resetAt := next
			for range calendar.ResetAfterIntervals {
				resetAt = addInterval(resetAt, calendar.IntervalUnit, calendar.IntervalCount)
			}
			if !now.Before(resetAt) {
				position, status = positionAtOrdinal(1, calendar)
				reset = true
			}
		}
		following, err := nextAvailableAt(calendar, now)
		return position, &following, reset, status, err
	}
	position, status := positionAtOrdinal(1, calendar)
	next, err := nextAvailableAt(calendar, now)
	return position, &next, false, status, err
}

func nextStepPosition(current uint32, calendar Calendar) (uint32, string) {
	for index, step := range calendar.Steps {
		if step.Position == current {
			return positionAtOrdinal(uint64(index+2), calendar)
		}
	}
	return 0, StatusCompleted
}

func positionAtOrdinal(ordinal uint64, calendar Calendar) (uint32, string) {
	if ordinal > 0 && ordinal <= uint64(len(calendar.Steps)) {
		return calendar.Steps[ordinal-1].Position, ""
	}
	last := calendar.Steps[len(calendar.Steps)-1].Position
	switch calendar.EndBehavior {
	case EndRestart:
		return calendar.Steps[0].Position, ""
	case EndRepeatLast:
		return last, ""
	default:
		return 0, StatusCompleted
	}
}

func stepAt(steps []Step, position uint32) (Step, bool) {
	for _, step := range steps {
		if step.Position == position {
			return step, true
		}
	}
	return Step{}, false
}

func nullableUint32(value *uint32) sql.NullInt32 {
	if value == nil {
		return sql.NullInt32{}
	}
	return sql.NullInt32{Int32: int32(*value), Valid: true}
}

func sqlNullUint32Ptr(value sql.NullInt32) *uint32 {
	if !value.Valid {
		return nil
	}
	result := uint32(value.Int32)
	return &result
}

func sqlNullTimePtr(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time
	return &result
}
