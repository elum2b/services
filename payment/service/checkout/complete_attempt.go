package checkout

import (
	"context"

	"github.com/elum2b/services/payment/repository"
)

func (a *Checkout) CompleteAttempt(ctx context.Context, params CompleteAttemptParams) (*CompleteAttemptResult, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx

	result, err := a.repository.CompleteAttempt(ctx, repository.CompleteAttemptParams{
		WorkspaceID:       params.WorkspaceID,
		AttemptID:         params.AttemptID,
		ProviderCode:      params.ProviderCode,
		ProviderPaymentID: params.ProviderPaymentID,
		AmountMinor:       params.AmountMinor,
		AssetCode:         params.AssetCode,
	})
	if err != nil {
		return nil, err
	}
	return &CompleteAttemptResult{
		OrderID:       result.OrderID,
		AttemptID:     result.AttemptID,
		FulfillmentID: uint64Ptr(result.FulfillmentID),
		AlreadyDone:   result.AlreadyDone,
	}, nil
}
