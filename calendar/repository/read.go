package repository

import (
	"context"
	"time"

	calendarsqlc "github.com/elum2b/services/calendar/sqlc"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
)

func (r *Repository) GetCalendar(ctx context.Context, workspaceID, ref, locale string) (Calendar, error) {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return Calendar{}, err
	}

	key := calendarCacheKey(calendarCacheUserCalendar, workspaceID, ref, locale)
	return sqlwrap.Query(ctx, r.db, sqlwrap.Params{
		Key:               key,
		Timeout:           r.timeout,
		CacheL1Delay:      r.cacheL1,
		CacheL2Delay:      r.cacheL2,
		CacheVersionScope: calendarCacheScope(calendarCacheUserCalendar, workspaceID),
	}, func(ctx context.Context) (Calendar, error) {
		id, calendarType := calendarReference(ref)
		rows, err := r.q.GetCalendarBundle(ctx, calendarsqlc.GetCalendarBundleParams{
			Locale: locale, WorkspaceID: workspaceID, ID: id, Type: calendarType,
		})
		if err != nil {
			return Calendar{}, err
		}
		if len(rows) == 0 {
			return Calendar{}, nil
		}
		first := rows[0]
		value := Calendar{
			ID: first.ID, WorkspaceID: first.WorkspaceID, Type: first.Type,
			Mode:                first.Mode,
			IntervalType:        first.IntervalType,
			IntervalUnit:        first.IntervalUnit,
			IntervalCount:       uint32(first.IntervalCount),
			ResetAfterIntervals: uint32(first.ResetAfterIntervals),
			EndBehavior:         first.EndBehavior,
			Timezone:            first.Timezone, HideFutureRewards: first.HideFutureRewards,
			IsActive: first.IsActive, StartAt: sqlwrap.NullTimePtr(first.StartAt),
			EndAt: sqlwrap.NullTimePtr(first.EndAt), DeletedAt: sqlwrap.NullTimePtr(first.DeletedAt),
			CreatedAt: first.CreatedAt, UpdatedAt: first.UpdatedAt,
			Steps: make([]Step, 0),
		}
		if first.LocalizationLocale.Valid {
			value.Localization = &Localization{
				WorkspaceID: first.WorkspaceID, CalendarID: first.ID,
				Locale: first.LocalizationLocale.String, Title: first.LocalizationTitle.String,
				Description: first.LocalizationDescription.String,
			}
		}
		for _, row := range rows {
			value.Steps = appendStep(value.Steps, row.StepID, row.StepPosition,
				row.RewardID, row.RewardItemKey, row.RewardType,
				row.RewardItemCount, row.RewardScale, row.RewardDurationUnit)
		}
		return value, nil
	})
}

func (r *Repository) ListActive(ctx context.Context, workspaceID, locale string, now time.Time) ([]Calendar, error) {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return nil, err
	}

	key := calendarCacheKey(calendarCacheUserCatalog, workspaceID, locale)
	catalog, err := sqlwrap.Query(ctx, r.db, sqlwrap.Params{
		Key:               key,
		Timeout:           r.timeout,
		CacheL1Delay:      r.cacheL1,
		CacheL2Delay:      r.cacheL2,
		CacheVersionScope: calendarCacheScope(calendarCacheUserCatalog, workspaceID),
	}, func(ctx context.Context) ([]Calendar, error) {
		rows, err := r.q.ListActiveCalendars(ctx, calendarsqlc.ListActiveCalendarsParams{
			Locale:      locale,
			WorkspaceID: workspaceID,
		})
		if err != nil {
			return nil, err
		}
		result := make([]Calendar, 0, len(rows))
		for _, row := range rows {
			value := Calendar{
				ID:          row.ID,
				WorkspaceID: row.WorkspaceID,
				Type:        row.Type,
				Mode:        row.Mode,
				IsActive:    row.IsActive,
				StartAt:     sqlwrap.NullTimePtr(row.StartAt),
				EndAt:       sqlwrap.NullTimePtr(row.EndAt),
				DeletedAt:   sqlwrap.NullTimePtr(row.DeletedAt),
			}
			if row.Locale.Valid {
				value.Localization = &Localization{
					WorkspaceID: row.WorkspaceID,
					CalendarID:  row.ID,
					Locale:      row.Locale.String,
					Title:       row.Title.String,
					Description: row.Description.String,
				}
			}
			result = append(result, value)
		}
		return result, nil
	})
	if err != nil {
		return nil, err
	}
	result := make([]Calendar, 0, len(catalog))
	for _, calendar := range catalog {
		if calendarVisibleAt(calendar, now) {
			result = append(result, calendar)
		}
	}
	return result, nil
}

func calendarVisibleAt(value Calendar, now time.Time) bool {
	if !value.IsActive || value.DeletedAt != nil {
		return false
	}
	if value.StartAt != nil && value.StartAt.After(now) {
		return false
	}
	return value.EndAt == nil || value.EndAt.After(now)
}
