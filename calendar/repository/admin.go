package repository

import (
	"context"
	"database/sql"
	"time"

	calendarsqlc "github.com/elum2b/services/calendar/sqlc"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
)

type SaveCalendarParams struct {
	ID                  string
	WorkspaceID         string
	Type                string
	Mode                string
	IntervalType        string
	IntervalUnit        string
	IntervalCount       uint32
	ResetAfterIntervals uint32
	EndBehavior         string
	Timezone            string
	HideFutureRewards   bool
	IsActive            bool
	StartAt             *time.Time
	EndAt               *time.Time
}

func (r *Repository) CreateCalendar(ctx context.Context, params SaveCalendarParams) error {
	err := r.WithTx(ctx, func(txRepo *Repository) error {
		if err := txRepo.lockWorkspaceMutation(ctx, params.WorkspaceID); err != nil {
			return err
		}

		return txRepo.q.AdminCreateCalendar(ctx, calendarsqlc.AdminCreateCalendarParams{
			ID:                  params.ID,
			WorkspaceID:         params.WorkspaceID,
			Type:                params.Type,
			Mode:                params.Mode,
			IntervalType:        params.IntervalType,
			IntervalUnit:        params.IntervalUnit,
			IntervalCount:       int32(params.IntervalCount),
			ResetAfterIntervals: int32(params.ResetAfterIntervals),
			EndBehavior:         params.EndBehavior,
			Timezone:            params.Timezone,
			HideFutureRewards:   params.HideFutureRewards,
			IsActive:            params.IsActive,
			StartAt:             nullableTime(params.StartAt),
			EndAt:               nullableTime(params.EndAt),
		})
	})
	if err != nil {
		return err
	}

	r.invalidateCalendarCache(params.WorkspaceID)
	return nil
}

func (r *Repository) UpdateCalendar(ctx context.Context, params SaveCalendarParams) (int64, error) {
	var rows int64
	err := r.WithTx(ctx, func(txRepo *Repository) error {
		if err := txRepo.lockWorkspaceMutation(ctx, params.WorkspaceID); err != nil {
			return err
		}

		var err error
		rows, err = txRepo.q.AdminUpdateCalendar(ctx, calendarsqlc.AdminUpdateCalendarParams{
			Type:                params.Type,
			Mode:                params.Mode,
			IntervalType:        params.IntervalType,
			IntervalUnit:        params.IntervalUnit,
			IntervalCount:       int32(params.IntervalCount),
			ResetAfterIntervals: int32(params.ResetAfterIntervals),
			EndBehavior:         params.EndBehavior,
			Timezone:            params.Timezone,
			HideFutureRewards:   params.HideFutureRewards,
			IsActive:            params.IsActive,
			StartAt:             nullableTime(params.StartAt),
			EndAt:               nullableTime(params.EndAt),
			WorkspaceID:         params.WorkspaceID,
			ID:                  params.ID,
		})
		return err
	})
	if err != nil || rows == 0 {
		return rows, err
	}
	r.invalidateCalendarCache(params.WorkspaceID)
	return rows, nil
}

func (r *Repository) GetCalendarDefinition(ctx context.Context, workspaceID, id string) (Calendar, error) {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return Calendar{}, err
	}

	key := calendarCacheKey(calendarCacheAdminCalendar, workspaceID, id)
	return sqlwrap.Query(ctx, r.db, sqlwrap.Params{
		Key:               key,
		Timeout:           r.timeout,
		CacheL1Delay:      r.cacheL1,
		CacheL2Delay:      r.cacheL2,
		CacheVersionScope: calendarCacheScope(calendarCacheAdminCalendar, workspaceID),
	}, func(ctx context.Context) (Calendar, error) {
		row, err := r.q.AdminGetCalendar(ctx, calendarsqlc.AdminGetCalendarParams{
			WorkspaceID: workspaceID,
			ID:          id,
		})
		if err != nil {
			return Calendar{}, err
		}
		return mapDefinition(row), nil
	})
}

func (r *Repository) ListCalendars(ctx context.Context, workspaceID string, limit, offset int32) ([]Calendar, error) {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return nil, err
	}

	limit, offset = normalizePage(limit, offset)
	key := calendarCacheKey(calendarCacheAdminList, workspaceID, limit, offset)
	return sqlwrap.Query(ctx, r.db, sqlwrap.Params{
		Key:               key,
		Timeout:           r.timeout,
		CacheL1Delay:      r.cacheL1,
		CacheL2Delay:      r.cacheL2,
		CacheVersionScope: calendarCacheScope(calendarCacheAdminList, workspaceID),
	}, func(ctx context.Context) ([]Calendar, error) {
		rows, err := r.q.AdminListCalendars(ctx, calendarsqlc.AdminListCalendarsParams{
			WorkspaceID: workspaceID,
			Limit:       limit,
			Offset:      offset,
		})
		if err != nil {
			return nil, err
		}
		result := make([]Calendar, 0, len(rows))
		for _, row := range rows {
			result = append(result, mapDefinition(row))
		}
		return result, nil
	})
}

func (r *Repository) SetCalendarActive(ctx context.Context, workspaceID, id string, active bool) (int64, error) {
	var rows int64
	err := r.WithTx(ctx, func(txRepo *Repository) error {
		if err := txRepo.lockWorkspaceMutation(ctx, workspaceID); err != nil {
			return err
		}

		var err error
		rows, err = txRepo.q.AdminSetCalendarActive(ctx, calendarsqlc.AdminSetCalendarActiveParams{
			IsActive:    active,
			WorkspaceID: workspaceID,
			ID:          id,
		})
		return err
	})
	if err != nil || rows == 0 {
		return rows, err
	}
	r.invalidateCalendarCache(workspaceID)
	return rows, nil
}

func (r *Repository) DeleteCalendar(ctx context.Context, workspaceID, id string) (int64, error) {
	var rows int64
	err := r.WithTx(ctx, func(txRepo *Repository) error {
		if err := txRepo.lockWorkspaceMutation(ctx, workspaceID); err != nil {
			return err
		}

		var err error
		rows, err = txRepo.q.AdminSoftDeleteCalendar(ctx, calendarsqlc.AdminSoftDeleteCalendarParams{
			WorkspaceID: workspaceID,
			ID:          id,
		})
		return err
	})
	if err != nil || rows == 0 {
		return rows, err
	}
	r.invalidateCalendarCache(workspaceID)
	return rows, nil
}

func (r *Repository) UpsertLocalization(ctx context.Context, value Localization) error {
	err := r.withWorkspaceMutation(ctx, value.WorkspaceID, func(txRepo *Repository) error {
		return txRepo.q.AdminUpsertLocalization(ctx, calendarsqlc.AdminUpsertLocalizationParams{
			WorkspaceID: value.WorkspaceID,
			CalendarID:  value.CalendarID,
			Locale:      value.Locale,
			Title:       value.Title,
			Description: value.Description,
		})
	})
	if err != nil {
		return err
	}

	r.invalidateCalendarCache(value.WorkspaceID)
	return nil
}

func (r *Repository) GetLocalization(
	ctx context.Context,
	workspaceID, calendarID, locale string,
) (Localization, error) {
	key := calendarCacheKey(calendarCacheAdminLocalization, workspaceID, calendarID, locale)
	return sqlwrap.Query(ctx, r.db, sqlwrap.Params{
		Key:               key,
		Timeout:           r.timeout,
		CacheL1Delay:      r.cacheL1,
		CacheL2Delay:      r.cacheL2,
		CacheVersionScope: calendarCacheScope(calendarCacheAdminLocalization, workspaceID),
	}, func(ctx context.Context) (Localization, error) {
		row, err := r.q.AdminGetLocalization(ctx, calendarsqlc.AdminGetLocalizationParams{
			WorkspaceID: workspaceID,
			CalendarID:  calendarID,
			Locale:      locale,
		})
		if err != nil {
			return Localization{}, err
		}
		return Localization{
			WorkspaceID: row.WorkspaceID,
			CalendarID:  row.CalendarID,
			Locale:      row.Locale,
			Title:       row.Title,
			Description: row.Description,
		}, nil
	})
}

func (r *Repository) ListLocalizations(ctx context.Context, workspaceID, calendarID string) ([]Localization, error) {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return nil, err
	}

	key := calendarCacheKey(calendarCacheAdminLocalizations, workspaceID, calendarID)
	return sqlwrap.Query(ctx, r.db, sqlwrap.Params{
		Key:               key,
		Timeout:           r.timeout,
		CacheL1Delay:      r.cacheL1,
		CacheL2Delay:      r.cacheL2,
		CacheVersionScope: calendarCacheScope(calendarCacheAdminLocalizations, workspaceID),
	}, func(ctx context.Context) ([]Localization, error) {
		rows, err := r.q.AdminListLocalizations(ctx, calendarsqlc.AdminListLocalizationsParams{
			WorkspaceID: workspaceID,
			CalendarID:  calendarID,
		})
		if err != nil {
			return nil, err
		}
		result := make([]Localization, 0, len(rows))
		for _, row := range rows {
			result = append(result, Localization{
				WorkspaceID: row.WorkspaceID,
				CalendarID:  row.CalendarID,
				Locale:      row.Locale,
				Title:       row.Title,
				Description: row.Description,
			})
		}
		return result, nil
	})
}

func (r *Repository) DeleteLocalization(ctx context.Context, workspaceID, calendarID, locale string) (int64, error) {
	var rows int64
	err := r.withWorkspaceMutation(ctx, workspaceID, func(txRepo *Repository) error {
		var err error
		rows, err = txRepo.q.AdminDeleteLocalization(ctx, calendarsqlc.AdminDeleteLocalizationParams{
			WorkspaceID: workspaceID,
			CalendarID:  calendarID,
			Locale:      locale,
		})
		return err
	})
	if err != nil || rows == 0 {
		return rows, err
	}

	r.invalidateCalendarCache(workspaceID)
	return rows, nil
}

func (r *Repository) CreateStep(ctx context.Context, workspaceID, calendarID string, position uint32) (uint64, error) {
	var id int64
	err := r.withWorkspaceMutation(ctx, workspaceID, func(txRepo *Repository) error {
		var err error
		id, err = txRepo.q.AdminCreateStep(ctx, calendarsqlc.AdminCreateStepParams{
			WorkspaceID: workspaceID,
			CalendarID:  calendarID,
			Position:    int32(position),
		})
		return err
	})
	if err != nil {
		return 0, err
	}

	r.invalidateCalendarCache(workspaceID)
	return uint64(id), nil
}

func (r *Repository) UpdateStep(
	ctx context.Context,
	workspaceID, calendarID string,
	id uint64,
	position uint32,
) (int64, error) {
	var rows int64
	err := r.withWorkspaceMutation(ctx, workspaceID, func(txRepo *Repository) error {
		var err error
		rows, err = txRepo.q.AdminUpdateStep(ctx, calendarsqlc.AdminUpdateStepParams{
			Position:    int32(position),
			WorkspaceID: workspaceID,
			CalendarID:  calendarID,
			ID:          int64(id),
		})
		return err
	})
	if err != nil || rows == 0 {
		return rows, err
	}

	r.invalidateCalendarCache(workspaceID)
	return rows, nil
}

func (r *Repository) DeleteStep(ctx context.Context, workspaceID, calendarID string, id uint64) (int64, error) {
	var rows int64
	err := r.withWorkspaceMutation(ctx, workspaceID, func(txRepo *Repository) error {
		var err error
		rows, err = txRepo.q.AdminDeleteStep(ctx, calendarsqlc.AdminDeleteStepParams{
			WorkspaceID: workspaceID,
			CalendarID:  calendarID,
			ID:          int64(id),
		})
		return err
	})
	if err != nil || rows == 0 {
		return rows, err
	}

	r.invalidateCalendarCache(workspaceID)
	return rows, nil
}

func (r *Repository) UpsertReward(
	ctx context.Context,
	workspaceID, calendarID string,
	stepID uint64,
	reward Reward,
	position uint32,
) (uint64, error) {
	var id int64
	err := r.withWorkspaceMutation(ctx, workspaceID, func(txRepo *Repository) error {
		var err error
		id, err = txRepo.q.AdminUpsertReward(ctx, calendarsqlc.AdminUpsertRewardParams{
			WorkspaceID:  workspaceID,
			CalendarID:   calendarID,
			StepID:       int64(stepID),
			ItemKey:      reward.Key,
			RewardType:   reward.Type,
			ItemCount:    reward.Quantity,
			Scale:        int16(reward.Scale),
			DurationUnit: nullableString(reward.Unit),
			Position:     int32(position),
		})
		return err
	})
	if err != nil {
		return 0, err
	}

	r.invalidateCalendarCache(workspaceID)
	return uint64(id), nil
}

func (r *Repository) UpdateReward(
	ctx context.Context,
	workspaceID, calendarID string,
	stepID, id uint64,
	reward Reward,
	position uint32,
) (int64, error) {
	var rows int64
	err := r.withWorkspaceMutation(ctx, workspaceID, func(txRepo *Repository) error {
		var err error
		rows, err = txRepo.q.AdminUpdateReward(ctx, calendarsqlc.AdminUpdateRewardParams{
			StepID:       int64(stepID),
			ItemKey:      reward.Key,
			RewardType:   reward.Type,
			ItemCount:    reward.Quantity,
			Scale:        int16(reward.Scale),
			DurationUnit: nullableString(reward.Unit),
			Position:     int32(position),
			WorkspaceID:  workspaceID,
			CalendarID:   calendarID,
			ID:           int64(id),
		})
		return err
	})
	if err != nil || rows == 0 {
		return rows, err
	}

	r.invalidateCalendarCache(workspaceID)
	return rows, nil
}

func (r *Repository) GetReward(ctx context.Context, workspaceID, calendarID string, id uint64) (Reward, error) {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return Reward{}, err
	}

	key := calendarCacheKey(calendarCacheAdminReward, workspaceID, calendarID, id)
	return sqlwrap.Query(ctx, r.db, sqlwrap.Params{
		Key:               key,
		Timeout:           r.timeout,
		CacheL1Delay:      r.cacheL1,
		CacheL2Delay:      r.cacheL2,
		CacheVersionScope: calendarCacheScope(calendarCacheAdminReward, workspaceID),
	}, func(ctx context.Context) (Reward, error) {
		row, err := r.q.AdminGetReward(ctx, calendarsqlc.AdminGetRewardParams{
			WorkspaceID: workspaceID,
			CalendarID:  calendarID,
			ID:          int64(id),
		})
		if err != nil {
			return Reward{}, err
		}
		return Reward{
			Key:      row.ItemKey,
			Type:     row.RewardType,
			Quantity: row.ItemCount,
			Scale:    uint16(row.Scale),
			Unit:     calendarDurationUnitPtr(row.DurationUnit),
		}, nil
	})
}

func (r *Repository) DeleteReward(ctx context.Context, workspaceID, calendarID string, id uint64) (int64, error) {
	var rows int64
	err := r.withWorkspaceMutation(ctx, workspaceID, func(txRepo *Repository) error {
		var err error
		rows, err = txRepo.q.AdminDeleteReward(ctx, calendarsqlc.AdminDeleteRewardParams{
			WorkspaceID: workspaceID,
			CalendarID:  calendarID,
			ID:          int64(id),
		})
		return err
	})
	if err != nil || rows == 0 {
		return rows, err
	}

	r.invalidateCalendarCache(workspaceID)
	return rows, nil
}

func nullableTime(value *time.Time) sql.NullTime {
	return sqlwrap.NullFromPtr(value, func(value time.Time) sql.NullTime {
		return sql.NullTime{Time: value, Valid: true}
	})
}
