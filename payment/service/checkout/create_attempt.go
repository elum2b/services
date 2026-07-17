package checkout

import (
	"context"

	"github.com/elum2b/services/payment/repository"
)

func (a *Checkout) CreateAttempt(ctx context.Context, params CreateAttemptParams) (*Attempt, error) {

	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx

	if err := params.Identity.Validate(); err != nil {
		return nil, err
	}

	attempt, err := a.repository.CreateUserAttempt(ctx, params.Identity, repository.AttemptCreateParams{
		OrderID:                params.OrderID,
		ProviderCode:           params.ProviderCode,
		ProviderPaymentID:      params.ProviderPaymentID,
		ProviderInvoiceID:      params.ProviderInvoiceID,
		ProviderChargeID:       params.ProviderChargeID,
		ProviderSubscriptionID: params.ProviderSubscriptionID,
		IdempotencyKey:         params.IdempotencyKey,
		ConfirmationURL:        params.ConfirmationURL,
		ReturnURL:              params.ReturnURL,
		ExpiresAt:              params.ExpiresAt,
	})
	if err != nil {
		return nil, err
	}

	return mapAttempt(attempt), nil

}
