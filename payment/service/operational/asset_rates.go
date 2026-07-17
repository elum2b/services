package operational

import (
	"context"

	"github.com/elum2b/services/internal/utils/contextutil"
	"github.com/elum2b/services/payment/repository"
)

func (o *Operational) UpdateAssetRate(
	ctx context.Context,
	params UpdateAssetRateParams,
) (UpdateAssetRateResult, error) {
	mergedCtx, cancel := contextutil.Merge(o.rootCtx, ctx)
	defer cancel()

	result, err := o.repository.UpdateAssetRate(mergedCtx, repository.AssetRateUpdateParams{
		AssetCode:              params.AssetCode,
		ReferenceAssetCode:     params.ReferenceAssetCode,
		ReferencePerAssetMinor: params.ReferencePerAssetMinor,
		Source:                 params.Source,
		ObservedAt:             params.ObservedAt,
	})
	if err != nil {
		return UpdateAssetRateResult{}, err
	}

	return UpdateAssetRateResult{
		UpdatedPrices:      result.UpdatedPrices,
		AffectedProducts:   result.AffectedProducts,
		AffectedWorkspaces: result.AffectedWorkspaces,
	}, nil
}

func (o *Operational) ConfigureAssetRateAutoUpdate(
	ctx context.Context,
	params ConfigureAssetRateAutoUpdateParams,
) error {
	mergedCtx, cancel := contextutil.Merge(o.rootCtx, ctx)
	defer cancel()

	return o.repository.ConfigureAssetRateAutoUpdate(mergedCtx, repository.AssetRateAutoUpdateParams{
		AssetCode:          params.AssetCode,
		ReferenceAssetCode: params.ReferenceAssetCode,
		Enabled:            params.Enabled,
		Source:             params.Source,
		SourceChainID:      params.SourceChainID,
		SourceTokenAddress: params.SourceTokenAddress,
	})
}
