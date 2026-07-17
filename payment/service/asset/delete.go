package asset

import "context"

func (a *Asset) Delete(ctx context.Context, code string) (int64, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx

	return a.repository.DeleteAsset(ctx, code)
}
