package admin

import (
	"context"

	paymentsqlc "github.com/elum2b/services/payment/sqlc"
)

func (a *Admin) GetAssetRate(
	ctx context.Context,
	assetCode string,
	referenceAssetCode string,
) (AssetRateModel, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	return a.repository.AdminGetAssetRate(mergedCtx, paymentsqlc.AdminGetAssetRateParams{
		AssetCode:          assetCode,
		ReferenceAssetCode: referenceAssetCode,
	})
}

func (a *Admin) ListAssetRates(
	ctx context.Context,
	params AssetRateListParams,
) ([]AssetRateModel, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	limit, offset := normalizePage(params.Page)
	return a.repository.AdminListAssetRates(mergedCtx, paymentsqlc.AdminListAssetRatesParams{
		Column1:            params.AssetCode,
		AssetCode:          params.AssetCode,
		Column3:            params.ReferenceAssetCode,
		ReferenceAssetCode: params.ReferenceAssetCode,
		Limit:              limit,
		Offset:             offset,
	})
}
