package admin

import (
	"context"

	paymentsqlc "github.com/elum2b/services/payment/sqlc"
)

func (a *Admin) ListProviders(ctx context.Context) ([]ProviderModel, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	return a.repository.ListProviders(ctx)
}

func (a *Admin) GetProvider(ctx context.Context, code string) (ProviderModel, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	return a.repository.AdminGetProvider(ctx, code)
}

func (a *Admin) ListAssets(ctx context.Context) ([]AssetModel, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	return a.repository.ListAssets(ctx)
}

func (a *Admin) GetAsset(ctx context.Context, code string) (AssetModel, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	return a.repository.AdminGetAsset(ctx, code)
}

func (a *Admin) ListProviderAssets(ctx context.Context, params ProviderAssetListParams) ([]ProviderAssetModel, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	limit, offset := normalizePage(params.Page)
	return a.repository.AdminListProviderAssets(ctx, paymentsqlc.AdminListProviderAssetsParams{
		Column1:      params.ProviderCode,
		ProviderCode: params.ProviderCode,
		Column3:      params.AssetCode,
		AssetCode:    params.AssetCode,
		Limit:        limit,
		Offset:       offset,
	})
}

func (a *Admin) GetProviderAsset(
	ctx context.Context,
	providerCode string,
	assetCode string,
) (ProviderAssetModel, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	return a.repository.GetProviderAsset(ctx, providerCode, assetCode)
}
