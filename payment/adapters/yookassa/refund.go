package yookassa

import (
	"context"
)

func (a *YooKassa) Execute(ctx context.Context, params RefundParams) (RefundResult, error) {

	if a == nil {
		return RefundResult{}, ErrNotInitialized
	}

	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx

	result, err := NewClient(params.Credentials).CreateRefund(ctx, createRefundRequest{
		PaymentID: params.PaymentID,
		Amount: Amount{
			Value:    formatRubMinor(params.AmountMinor),
			Currency: params.AssetCode,
		},
		Description: params.Description,
	}, params.IdempotencyKey)
	if err != nil {
		return RefundResult{}, err
	}
	return RefundResult{
		ProviderRefundID: result.ID,
		Status:           result.Status,
	}, nil

}
