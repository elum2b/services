package product

import "context"

func (a *Product) DeletePrice(ctx context.Context, workspaceID string, id uint64) (int64, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx

	return a.repository.DeleteProductPrice(ctx, workspaceID, id)
}
