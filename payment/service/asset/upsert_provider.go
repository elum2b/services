package asset

import (
	"context"

	"github.com/elum2b/services/payment/repository"
)

func (a *Asset) UpsertProvider(ctx context.Context, params ProviderUpsertParams) error {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx

	return a.repository.UpsertProviderAsset(ctx, repository.ProviderAssetUpsertParams{
		ProviderCode:    params.ProviderCode,
		AssetCode:       params.AssetCode,
		MinAmountMinor:  params.MinAmountMinor,
		MaxAmountMinor:  params.MaxAmountMinor,
		MerchantAccount: params.MerchantAccount,
		IsActive:        params.IsActive,
	})
}
