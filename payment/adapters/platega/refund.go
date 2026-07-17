package platega

import (
	"context"
)

type RefundParams struct {
	Executor       RefundExecutor
	TransactionID  string
	AmountMinor    uint64
	AssetCode      string
	Reason         string
	IdempotencyKey string
}

type RefundResult struct {
	ProviderRefundID string `json:"provider_refund_id,omitempty"`
	Status           string `json:"status"`
}

type RefundExecutor func(context.Context, RefundParams) (RefundResult, error)

func (a *Platega) Execute(ctx context.Context, params RefundParams) (RefundResult, error) {

	if a == nil {
		return RefundResult{}, ErrNotInitialized
	}

	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	if params.Executor != nil {
		return params.Executor(ctx, params)
	}

	return RefundResult{}, ErrRefundUnsupported

}
