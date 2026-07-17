package product

import "context"

func (a *Product) DeleteGroup(ctx context.Context, workspaceID string, code string) (int64, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx

	return a.repository.DeleteProductGroup(ctx, workspaceID, code)
}
