package repository

import (
	"context"
	"database/sql"
	"time"

	calendarsqlc "github.com/elum2b/services/calendar/sqlc"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
)

func (r *Repository) Export(ctx context.Context, workspaceID string, req ExportRequest) (ExportPackage, error) {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return ExportPackage{}, err
	}
	now := req.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var calendars []calendarsqlc.CalendarDefinition
	var localizationRows []calendarsqlc.CalendarLocalization
	var stepRows []calendarsqlc.ListExportStepsWithRewardsRow
	if err := r.WithTx(ctx, func(txRepo *Repository) error {
		if _, err := txRepo.executor.ExecContext(
			ctx,
			"SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY",
		); err != nil {
			return err
		}

		var err error
		calendars, err = txRepo.q.ListExportCalendars(ctx, workspaceID)
		if err != nil {
			return err
		}

		localizationRows, err = txRepo.q.ListExportLocalizations(ctx, workspaceID)
		if err != nil {
			return err
		}

		stepRows, err = txRepo.q.ListExportStepsWithRewards(ctx, workspaceID)
		return err
	}); err != nil {
		return ExportPackage{}, err
	}

	localizations := mapExportLocalizations(localizationRows)
	steps := mapExportSteps(stepRows)
	out := ExportPackage{
		Format:    ExportFormat,
		Service:   "calendar",
		CreatedAt: now.UTC(),
		Calendars: make([]ExportCalendar, 0, len(calendars)),
	}
	for _, calendar := range calendars {
		item := ExportCalendar{
			ID:                  calendar.ID,
			Type:                calendar.Type,
			Mode:                calendar.Mode,
			IntervalType:        calendar.IntervalType,
			IntervalUnit:        calendar.IntervalUnit,
			IntervalCount:       uint32(calendar.IntervalCount),
			ResetAfterIntervals: uint32(calendar.ResetAfterIntervals),
			EndBehavior:         calendar.EndBehavior,
			Timezone:            calendar.Timezone,
			HideFutureRewards:   calendar.HideFutureRewards,
			IsActive:            calendar.IsActive,
			StartAt:             sqlwrap.NullTimePtr(calendar.StartAt),
			EndAt:               sqlwrap.NullTimePtr(calendar.EndAt),
			Localization:        localizations[calendar.ID],
			Steps:               steps[calendar.ID],
		}
		out.Calendars = append(out.Calendars, item)
	}
	return out, nil
}

func mapExportLocalizations(rows []calendarsqlc.CalendarLocalization) map[string]map[string]ExportText {
	result := make(map[string]map[string]ExportText)
	for _, row := range rows {
		if result[row.CalendarID] == nil {
			result[row.CalendarID] = make(map[string]ExportText)
		}
		result[row.CalendarID][row.Locale] = ExportText{
			Title:       row.Title,
			Description: row.Description,
		}
	}
	return result
}

func mapExportSteps(rows []calendarsqlc.ListExportStepsWithRewardsRow) map[string][]ExportStep {
	result := make(map[string][]ExportStep)
	var lastCalendarID string
	var lastStepID int64
	for _, row := range rows {
		if row.CalendarID != lastCalendarID || row.StepID != lastStepID {
			result[row.CalendarID] = append(result[row.CalendarID], ExportStep{
				Position: uint32(row.StepPosition),
			})
			lastCalendarID, lastStepID = row.CalendarID, row.StepID
		}
		if row.RewardItemKey.Valid {
			steps := result[row.CalendarID]
			index := len(steps) - 1
			steps[index].Rewards = append(steps[index].Rewards, ExportReward{
				Key:      row.RewardItemKey.String,
				Type:     row.RewardType.String,
				Quantity: row.RewardItemCount.Int64,
				Scale:    uint16FromSQL(row.RewardScale),
				Unit:     nullStringPtr(row.RewardDurationUnit),
				Position: uint32FromSQL(row.RewardPosition),
			})
			result[row.CalendarID] = steps
		}
	}
	return result
}

func nullStringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func uint16FromSQL(value sql.NullInt16) uint16 {
	if !value.Valid || value.Int16 < 0 {
		return 0
	}
	return uint16(value.Int16)
}

func uint32FromSQL(value sql.NullInt32) uint32 {
	if !value.Valid || value.Int32 < 0 {
		return 0
	}
	return uint32(value.Int32)
}
