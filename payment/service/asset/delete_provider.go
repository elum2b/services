package asset

import "context"

func (a *Asset) DeleteProvider(ctx context.Context, providerCode string, assetCode string) (int64, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx

	return a.repository.DeleteProviderAsset(ctx, providerCode, assetCode)
}
