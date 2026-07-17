package admin

import (
	"context"
	"strings"

	callbackutil "github.com/elum2b/services/internal/utils/callback"
)

type CallbackEventListParams struct {
	WorkspaceID string
	EventType   string
	Status      string
	Page        Page
}

func (a *Admin) ListCallbackEvents(ctx context.Context, params CallbackEventListParams) ([]callbackutil.Event, error) {

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	limit, offset := normalizePage(params.Page)
	return a.callbacks.AdminListEvents(mergedCtx, callbackutil.AdminListEventsParams{
		WorkspaceID:   params.WorkspaceID,
		SourceService: "cpa",
		EventType:     params.EventType,
		Status:        params.Status,
		Limit:         limit,
		Offset:        offset,
	})

}

func (a *Admin) GetCallbackEvent(ctx context.Context, workspaceID string, id uint64) (callbackutil.Event, error) {
	if id == 0 {
		return callbackutil.Event{}, ErrCallbackEventIDRequired
	}

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	return a.callbacks.GetEvent(mergedCtx, workspaceID, id)

}

func (a *Admin) RetryCallbackEventNow(ctx context.Context, workspaceID string, id uint64) (int64, error) {
	if id == 0 {
		return 0, ErrCallbackEventIDRequired
	}

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	return a.callbacks.AdminRetryEventNow(mergedCtx, workspaceID, id)

}

func (a *Admin) MarkCallbackEventOK(ctx context.Context, workspaceID string, id uint64) (int64, error) {
	if id == 0 {
		return 0, ErrCallbackEventIDRequired
	}

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	return a.callbacks.AdminMarkEventOK(mergedCtx, workspaceID, id)

}

func (a *Admin) MarkCallbackEventReject(ctx context.Context, workspaceID string, id uint64, reason string) (int64, error) {
	if id == 0 {
		return 0, ErrCallbackEventIDRequired
	}
	if strings.TrimSpace(reason) == "" {
		return 0, ErrCallbackRejectReasonRequired
	}

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	return a.callbacks.AdminMarkEventReject(mergedCtx, workspaceID, id, reason)

}

func (a *Admin) ResetExpiredCallbackProcessing(ctx context.Context, workspaceID string) (int64, error) {

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	return a.callbacks.AdminResetExpiredProcessing(mergedCtx, workspaceID)

}
