package telegramstars

import (
	"context"
)

func (a *TelegramStars) Execute(ctx context.Context, params RefundParams) (RefundResult, error) {
	if a == nil {
		return RefundResult{}, ErrNotInitialized
	}

	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	if err := NewClient(params.Credentials).RefundStarPayment(ctx, refundStarPaymentRequest{
		UserID:                  params.UserID,
		TelegramPaymentChargeID: params.TelegramPaymentChargeID,
	}); err != nil {
		return RefundResult{}, err
	}
	return RefundResult{Status: "succeeded"}, nil
}
