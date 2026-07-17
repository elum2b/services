package product

import "context"

func (a *Product) Create(ctx context.Context, params UpsertParams) error {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx

	return a.Upsert(ctx, params)
}
