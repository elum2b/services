package admin

import (
	"context"
	"math"
	"strings"
	"time"

	services "github.com/elum2b/services"
	"github.com/elum2b/services/calendar/repository"
	"github.com/elum2b/services/calendar/service/user"
	"github.com/google/uuid"
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

func (a *Admin) CreateCalendar(ctx context.Context, params SaveCalendarParams) (string, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	if err := validateCalendar(&params); err != nil {
		return "", err
	}
	if params.ID == "" {
		params.ID = uuid.NewString()
	}
	return params.ID, a.repository.CreateCalendar(mergedCtx, repository.SaveCalendarParams(params))
}

func (a *Admin) UpdateCalendar(ctx context.Context, params SaveCalendarParams) (int64, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	if params.ID == "" {
		return 0, ErrCalendarIDRequired
	}
	if err := validateCalendar(&params); err != nil {
		return 0, err
	}
	return a.repository.UpdateCalendar(mergedCtx, repository.SaveCalendarParams(params))
}

func (a *Admin) GetCalendar(ctx context.Context, workspaceID, id string) (CalendarModel, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	value, err := a.repository.GetCalendar(mergedCtx, workspaceID, id, "")
	if err != nil {
		return CalendarModel{}, err
	}
	localizations, err := a.repository.ListLocalizations(mergedCtx, workspaceID, id)
	if err != nil {
		return CalendarModel{}, err
	}
	result := mapCalendar(value)
	result.Localizations = make([]LocalizationModel, 0, len(localizations))
	for _, item := range localizations {
		result.Localizations = append(result.Localizations, LocalizationModel{
			Locale: item.Locale, Title: item.Title, Description: item.Description,
		})
	}
	return result, nil
}

func (a *Admin) ListCalendars(ctx context.Context, workspaceID string, page Page) ([]CalendarModel, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	limit, offset := normalizePage(page)
	values, err := a.repository.ListCalendars(mergedCtx, workspaceID, limit, offset)
	if err != nil {
		return nil, err
	}
	result := make([]CalendarModel, 0, len(values))
	for _, value := range values {
		result = append(result, mapCalendar(value))
	}
	return result, nil
}

func (a *Admin) SetCalendarActive(ctx context.Context, workspaceID, id string, active bool) (int64, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	return a.repository.SetCalendarActive(mergedCtx, workspaceID, id, active)
}

func (a *Admin) DeleteCalendar(ctx context.Context, workspaceID, id string) (int64, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	return a.repository.DeleteCalendar(mergedCtx, workspaceID, id)
}

func validateCalendar(params *SaveCalendarParams) error {
	if err := services.ValidateWorkspaceID(params.WorkspaceID); err != nil {
		return err
	}
	if strings.TrimSpace(params.Type) == "" {
		return ErrCalendarScopeRequired
	}
	if params.IntervalCount == 0 {
		params.IntervalCount = 1
	}
	if params.ResetAfterIntervals == 0 {
		params.ResetAfterIntervals = 1
	}
	if params.IntervalCount > math.MaxInt32 || params.ResetAfterIntervals > math.MaxInt32 {
		return ErrCalendarNumberOutOfRange
	}
	if params.Timezone == "" {
		params.Timezone = "UTC"
	}
	if _, err := time.LoadLocation(params.Timezone); err != nil {
		return ErrCalendarTimezoneInvalid
	}
	if params.StartAt != nil && params.EndAt != nil && !params.StartAt.Before(*params.EndAt) {
		return ErrCalendarRangeInvalid
	}
	if params.Mode != repository.ModeInterval && params.Mode != repository.ModeSequential &&
		params.Mode != repository.ModeSequentialReset {
		return ErrCalendarModeInvalid
	}
	if params.IntervalType != repository.IntervalCalendar && params.IntervalType != repository.IntervalFloating {
		return ErrCalendarIntervalTypeInvalid
	}
	switch params.IntervalUnit {
	case "second", "minute", "hour", "day", "week", "month":
	default:
		return ErrCalendarIntervalUnitInvalid
	}
	if params.EndBehavior != repository.EndRestart && params.EndBehavior != repository.EndRepeatLast &&
		params.EndBehavior != repository.EndStop {
		return ErrCalendarEndBehaviorInvalid
	}
	return nil
}

func mapCalendar(value repository.Calendar) CalendarModel {
	model := user.CalendarModel{
		ID: value.ID, Type: value.Type, Mode: value.Mode, IntervalType: value.IntervalType,
		IntervalUnit: value.IntervalUnit, IntervalCount: value.IntervalCount,
		ResetAfterIntervals: value.ResetAfterIntervals, EndBehavior: value.EndBehavior,
		Timezone: value.Timezone, HideFutureRewards: value.HideFutureRewards,
		IsActive: value.IsActive, StartAt: value.StartAt, EndAt: value.EndAt,
		Steps: make([]user.StepModel, 0, len(value.Steps)),
	}
	for _, step := range value.Steps {
		item := user.StepModel{
			ID:       step.ID,
			Position: step.Position,
			Rewards:  make([]user.RewardModel, 0, len(step.Rewards)),
		}
		for _, reward := range step.Rewards {
			item.Rewards = append(item.Rewards, user.RewardModel{
				Key: reward.Key, Type: reward.Type, Quantity: reward.Quantity, Scale: reward.Scale, Unit: reward.Unit,
			})
		}
		model.Steps = append(model.Steps, item)
	}
	return CalendarModel{
		CalendarModel: model, DeletedAt: value.DeletedAt,
		CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	}
}
