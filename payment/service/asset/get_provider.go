package asset

import (
	"context"
)

func (a *Asset) GetProvider(
	ctx context.Context,
	providerCode string,
	assetCode string,
) (ProviderModel, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx

	return a.repository.GetProviderAsset(ctx, providerCode, assetCode)
}
