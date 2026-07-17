package product

import "context"

func (a *Product) RemoveItem(ctx context.Context, workspaceID string, productID string, itemID string) (int64, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx

	return a.repository.DeleteProductItem(ctx, workspaceID, productID, itemID)
}
