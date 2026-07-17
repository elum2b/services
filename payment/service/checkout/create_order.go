package checkout

import (
	"context"

	"github.com/elum2b/services/payment/repository"
)

func (a *Checkout) CreateOrder(ctx context.Context, params CreateOrderParams) (*Order, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()

	if err := params.Identity.Validate(); err != nil {
		return nil, err
	}

	order, err := a.repository.CreateOrder(mergedCtx, repository.OrderCreateParams{
		AppID:               params.Identity.AppID,
		WorkspaceID:         params.Identity.WorkspaceID,
		PlatformID:          params.Identity.PlatformID,
		PlatformUserID:      params.Identity.PlatformUserID,
		InternalUserID:      params.InternalUserID,
		PayerPlatformID:     actorPlatformID(params.Payer),
		PayerPlatformUserID: actorPlatformUserID(params.Payer),
		PayerInternalUserID: actorInternalUserID(params.Payer),
		ProductID:           params.ProductID,
		Quantity:            params.Quantity,
		AssetCode:           params.AssetCode,
		Locale:              params.Locale,
		ReservedUntil:       params.ReservedUntil,
		ExpiresAt:           params.ExpiresAt,
	})
	if err != nil {
		return nil, err
	}
	return mapOrder(order), nil
}
