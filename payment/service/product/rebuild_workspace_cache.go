package product

import "context"

func (a *Product) RebuildWorkspaceCache(ctx context.Context, workspaceID string) error {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx

	return a.repository.RebuildWorkspaceProductCache(ctx, workspaceID)
}
