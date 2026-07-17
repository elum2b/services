package checkout

import (
	"context"

	"github.com/elum2b/services/payment/repository"
)

func (a *Checkout) CreateEvent(ctx context.Context, params CreateEventParams) (uint64, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx

	return a.repository.CreateEvent(ctx, repository.EventCreateParams{
		WorkspaceID:       params.WorkspaceID,
		ProviderCode:      params.ProviderCode,
		AttemptID:         params.AttemptID,
		OrderID:           params.OrderID,
		ProviderEventID:   params.ProviderEventID,
		ProviderPaymentID: params.ProviderPaymentID,
		EventType:         params.EventType,
		EventStatus:       params.EventStatus,
		PayloadHash:       params.PayloadHash,
		SignatureValid:    params.SignatureValid,
	})
}
