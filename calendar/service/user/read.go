package user

import (
	"context"
	"time"

	services "github.com/elum2b/services"
	"github.com/elum2b/services/calendar/repository"
)

func (u *User) ListActive(ctx context.Context, params ListActiveParams) ([]ActiveCalendarModel, error) {
	mergedCtx, cancel := u.withContext(ctx)
	defer cancel()

	if err := services.ValidateWorkspaceID(params.WorkspaceID); err != nil {
		return nil, err
	}

	now := params.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	values, err := u.repository.ListActive(mergedCtx, params.WorkspaceID, params.Locale, now)
	if err != nil {
		return nil, err
	}
	result := make([]ActiveCalendarModel, 0, len(values))
	for _, value := range values {
		item := ActiveCalendarModel{
			ID: value.ID, Type: value.Type, Mode: value.Mode, IsActive: value.IsActive,
			StartAt: value.StartAt, EndAt: value.EndAt,
		}
		if value.Localization != nil {
			item.Title = value.Localization.Title
			item.Description = value.Localization.Description
		}
		result = append(result, item)
	}
	return result, nil
}

func (u *User) GetCalendar(ctx context.Context, params GetCalendarParams) (CalendarModel, error) {
	mergedCtx, cancel := u.withContext(ctx)
	defer cancel()

	if err := params.Identity.Validate(); err != nil {
		return CalendarModel{}, err
	}

	value, err := u.repository.GetCalendar(mergedCtx, params.Identity.WorkspaceID, params.Ref, params.Locale)
	if err != nil {
		return CalendarModel{}, err
	}
	now := params.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if !calendarVisibleAt(value, now) {
		return CalendarModel{}, nil
	}

	result := mapCalendar(value)
	if !value.HideFutureRewards || value.ID == "" {
		return result, nil
	}
	progress, err := u.repository.GetProgress(mergedCtx, repositoryIdentity(params.Identity), value.ID)
	if err != nil {
		return CalendarModel{}, err
	}
	maxPosition := uint32(1)
	if progress != nil {
		maxPosition = progress.CurrentPosition + 1
	}
	filtered := result.Steps[:0]
	for _, step := range result.Steps {
		if step.Position <= maxPosition {
			filtered = append(filtered, step)
		}
	}
	result.Steps = filtered
	return result, nil
}

func calendarVisibleAt(value repository.Calendar, now time.Time) bool {
	if value.ID == "" || !value.IsActive || value.DeletedAt != nil {
		return false
	}
	if value.StartAt != nil && value.StartAt.After(now) {
		return false
	}
	return value.EndAt == nil || value.EndAt.After(now)
}

func (u *User) GetProgress(ctx context.Context, params GetProgressParams) (*ProgressModel, error) {
	mergedCtx, cancel := u.withContext(ctx)
	defer cancel()

	if err := params.Identity.Validate(); err != nil {
		return nil, err
	}

	value, err := u.repository.GetProgress(mergedCtx, repositoryIdentity(params.Identity), params.CalendarID)
	if err != nil || value == nil {
		return nil, err
	}
	result := mapProgress(*value)
	return &result, nil
}
