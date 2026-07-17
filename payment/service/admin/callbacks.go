package admin

import (
	"context"

	callbackutil "github.com/elum2b/services/internal/utils/callback"
)

func (a *Admin) ListCallbackEvents(ctx context.Context, params CallbackEventListParams) ([]callbackutil.Event, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	limit, offset := normalizePage(params.Page)
	return a.callbacks.AdminListEvents(ctx, callbackutil.AdminListEventsParams{
		WorkspaceID:   params.WorkspaceID,
		SourceService: params.SourceService,
		EventType:     params.EventType,
		Status:        params.Status,
		Limit:         limit,
		Offset:        offset,
	})
}

func (a *Admin) GetCallbackEvent(ctx context.Context, workspaceID string, id uint64) (callbackutil.Event, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	return a.callbacks.GetEvent(ctx, workspaceID, id)
}

func (a *Admin) RetryCallbackEventNow(ctx context.Context, workspaceID string, id uint64) (int64, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	return a.callbacks.AdminRetryEventNow(ctx, workspaceID, id)
}

func (a *Admin) MarkCallbackEventOK(ctx context.Context, workspaceID string, id uint64) (int64, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	return a.callbacks.AdminMarkEventOK(ctx, workspaceID, id)
}

func (a *Admin) MarkCallbackEventReject(
	ctx context.Context,
	workspaceID string,
	id uint64,
	reason string,
) (int64, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	return a.callbacks.AdminMarkEventReject(ctx, workspaceID, id, reason)
}

func (a *Admin) ResetExpiredCallbackProcessing(ctx context.Context, workspaceID string) (int64, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	return a.callbacks.AdminResetExpiredProcessing(ctx, workspaceID)
}
