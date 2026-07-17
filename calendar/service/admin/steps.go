package admin

import (
	"context"
	"math"

	services "github.com/elum2b/services"
)

type SaveStepParams struct {
	WorkspaceID string
	CalendarID  string
	ID          uint64
	Position    uint32
}

func (a *Admin) CreateStep(ctx context.Context, params SaveStepParams) (uint64, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	if err := services.ValidateWorkspaceID(params.WorkspaceID); err != nil {
		return 0, err
	}
	if params.CalendarID == "" || params.Position == 0 {
		return 0, ErrStepCreateInvalid
	}
	if params.Position > math.MaxInt32 {
		return 0, ErrCalendarNumberOutOfRange
	}

	return a.repository.CreateStep(mergedCtx, params.WorkspaceID, params.CalendarID, params.Position)
}

func (a *Admin) UpdateStep(ctx context.Context, params SaveStepParams) (int64, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	if err := services.ValidateWorkspaceID(params.WorkspaceID); err != nil {
		return 0, err
	}
	if params.CalendarID == "" || params.ID == 0 || params.Position == 0 {
		return 0, ErrStepUpdateInvalid
	}
	if params.ID > math.MaxInt64 || params.Position > math.MaxInt32 {
		return 0, ErrCalendarNumberOutOfRange
	}

	return a.repository.UpdateStep(
		mergedCtx, params.WorkspaceID, params.CalendarID, params.ID, params.Position,
	)
}

func (a *Admin) DeleteStep(ctx context.Context, workspaceID, calendarID string, id uint64) (int64, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	if err := services.ValidateWorkspaceID(workspaceID); err != nil {
		return 0, err
	}
	if calendarID == "" || id == 0 {
		return 0, ErrStepUpdateInvalid
	}
	if id > math.MaxInt64 {
		return 0, ErrCalendarNumberOutOfRange
	}

	return a.repository.DeleteStep(mergedCtx, workspaceID, calendarID, id)
}
