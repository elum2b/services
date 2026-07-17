package checkout

import (
	"context"

	"github.com/elum2b/services/payment/repository"
)

func (a *Checkout) CreateOrderByKey(ctx context.Context, params CreateOrderByKeyParams) (*Order, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx

	order, err := a.repository.CreateOrderByKey(ctx, repository.OrderCreateByKeyParams{
		Key:                 params.Key,
		PayerPlatformID:     actorPlatformID(params.Payer),
		PayerPlatformUserID: actorPlatformUserID(params.Payer),
		PayerInternalUserID: actorInternalUserID(params.Payer),
		AssetCode:           params.AssetCode,
		Quantity:            params.Quantity,
		Locale:              params.Locale,
		ReservedUntil:       params.ReservedUntil,
		ExpiresAt:           params.ExpiresAt,
	})
	if err != nil {
		return nil, err
	}
	return mapOrder(order), nil
}
