package ton

import (
	"context"
)

type RefundParams struct {
	Executor       RefundExecutor
	Network        string
	AssetCode      string
	Destination    string
	AmountMinor    uint64
	Comment        string
	IdempotencyKey string
}

type RefundResult struct {
	ProviderRefundID string `json:"provider_refund_id,omitempty"`
	Status           string `json:"status"`
}

type RefundExecutor func(context.Context, RefundParams) (RefundResult, error)

func (a *TON) Execute(ctx context.Context, params RefundParams) (RefundResult, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	if params.Executor != nil {
		return params.Executor(ctx, params)
	}
	return RefundResult{}, ErrRefundUnsupported
}
