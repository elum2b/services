package admin

import (
	"context"
	services "github.com/elum2b/services"
	"github.com/elum2b/services/collections/repository"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/elum2b/services/collections/model"

	callbackutil "github.com/elum2b/services/internal/utils/callback"
)

func callbackDTO(e callbackutil.Event) model.CallbackEvent {
	return model.CallbackEvent{
		ID:           e.ID,
		WorkspaceID:  e.WorkspaceID,
		EventKey:     e.EventKey,
		Status:       model.CallbackStatus(e.Status),
		Payload:      e.Payload,
		AttemptCount: e.AttemptCount,
	}
}
func (a *Admin) ListCallbackEvents(ctx context.Context, p model.CallbackParams) ([]model.CallbackEvent, error) {
	if err := validateCallback(p, false, false); err != nil {
		return nil, err
	}
	ctx, cancel := a.withContext(ctx)
	defer cancel()
	limit, offset := normalizePage(p.Page)
	rows, err := a.callbacks.AdminListEvents(ctx, callbackutil.AdminListEventsParams{
		WorkspaceID:   p.WorkspaceID,
		SourceService: "collections",
		EventType:     "collections.ownership_changed",
		Status:        string(p.Status),
		Limit:         limit,
		Offset:        offset,
	})
	if err != nil {
		return nil, err
	}
	out := make([]model.CallbackEvent, 0, len(rows))
	for _, row := range rows {
		out = append(out, callbackDTO(row))
	}
	return out, nil
}
func (a *Admin) GetCallbackEvent(ctx context.Context, p model.CallbackParams) (model.CallbackEvent, error) {
	if err := validateCallback(p, true, false); err != nil {
		return model.CallbackEvent{}, err
	}
	ctx, cancel := a.withContext(ctx)
	defer cancel()
	row, err := a.callbacks.GetEvent(ctx, p.WorkspaceID, p.ID)
	return callbackDTO(row), repository.DomainError(err)
}
func (a *Admin) RetryCallbackEventNow(ctx context.Context, p model.CallbackParams) (int64, error) {
	if err := validateCallback(p, true, false); err != nil {
		return 0, err
	}
	ctx, cancel := a.withContext(ctx)
	defer cancel()
	return a.callbacks.AdminRetryEventNow(ctx, p.WorkspaceID, p.ID)
}
func (a *Admin) MarkCallbackEventOK(ctx context.Context, p model.CallbackParams) (int64, error) {
	if err := validateCallback(p, true, false); err != nil {
		return 0, err
	}
	ctx, cancel := a.withContext(ctx)
	defer cancel()
	return a.callbacks.AdminMarkEventOK(ctx, p.WorkspaceID, p.ID)
}
func (a *Admin) MarkCallbackEventReject(ctx context.Context, p model.CallbackParams) (int64, error) {
	if err := validateCallback(p, true, true); err != nil {
		return 0, err
	}
	ctx, cancel := a.withContext(ctx)
	defer cancel()
	return a.callbacks.AdminMarkEventReject(ctx, p.WorkspaceID, p.ID, p.Reason)
}
func (a *Admin) ResetExpiredCallbackProcessing(ctx context.Context, p model.CallbackParams) (int64, error) {
	if err := validateCallback(p, false, false); err != nil {
		return 0, err
	}
	ctx, cancel := a.withContext(ctx)
	defer cancel()
	return a.callbacks.AdminResetExpiredProcessing(ctx, p.WorkspaceID)
}

func validateCallback(p model.CallbackParams, id, reason bool) error {
	if err := services.ValidateWorkspaceID(p.WorkspaceID); err != nil {
		return err
	}
	if id && (p.ID == 0 || p.ID > math.MaxInt64) {
		return repository.ErrInvalid
	}
	switch p.Status {
	case "", model.CallbackPending, model.CallbackProcessing, model.CallbackOK, model.CallbackRejected:
	default:
		return repository.ErrInvalid
	}
	if reason && (strings.TrimSpace(p.Reason) == "" || len(p.Reason) > 4096 || !utf8.ValidString(p.Reason) || strings.ContainsRune(p.Reason, 0)) {
		return repository.ErrInvalid
	}
	return nil
}
