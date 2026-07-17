package operational

import (
	"context"

	"github.com/elum2b/services/internal/utils/contextutil"
	"github.com/elum2b/services/payment/repository"
)

func (o *Operational) UpsertProvider(ctx context.Context, params ProviderUpsertParams) error {
	mergedCtx, cancel := contextutil.Merge(o.rootCtx, ctx)
	defer cancel()

	return o.repository.UpsertProvider(mergedCtx, repository.ProviderUpsertParams{
		Code:             params.Code,
		Title:            params.Title,
		ProviderKind:     params.ProviderKind,
		SupportsCreate:   params.SupportsCreate,
		SupportsRedirect: params.SupportsRedirect,
		SupportsWebhook:  params.SupportsWebhook,
		SupportsRefund:   params.SupportsRefund,
		IsActive:         params.IsActive,
	})
}

func (o *Operational) DeleteProvider(ctx context.Context, code string) (int64, error) {
	mergedCtx, cancel := contextutil.Merge(o.rootCtx, ctx)
	defer cancel()

	return o.repository.AdminDeleteProvider(mergedCtx, code)
}

func (o *Operational) UpsertAsset(ctx context.Context, params AssetUpsertParams) error {
	mergedCtx, cancel := contextutil.Merge(o.rootCtx, ctx)
	defer cancel()

	return o.repository.UpsertAsset(mergedCtx, repository.AssetUpsertParams{
		Code:            params.Code,
		Title:           params.Title,
		AssetKind:       params.AssetKind,
		Scale:           params.Scale,
		Chain:           params.Chain,
		Network:         params.Network,
		ContractAddress: params.ContractAddress,
		IsActive:        params.IsActive,
	})
}

func (o *Operational) DeleteAsset(ctx context.Context, code string) (int64, error) {
	mergedCtx, cancel := contextutil.Merge(o.rootCtx, ctx)
	defer cancel()

	return o.repository.DeleteAsset(mergedCtx, code)
}

func (o *Operational) UpsertProviderAsset(ctx context.Context, params ProviderAssetUpsertParams) error {
	mergedCtx, cancel := contextutil.Merge(o.rootCtx, ctx)
	defer cancel()

	return o.repository.UpsertProviderAsset(mergedCtx, repository.ProviderAssetUpsertParams{
		ProviderCode:    params.ProviderCode,
		AssetCode:       params.AssetCode,
		MinAmountMinor:  params.MinAmountMinor,
		MaxAmountMinor:  params.MaxAmountMinor,
		MerchantAccount: params.MerchantAccount,
		IsActive:        params.IsActive,
	})
}

func (o *Operational) DeleteProviderAsset(
	ctx context.Context,
	providerCode string,
	assetCode string,
) (int64, error) {
	mergedCtx, cancel := contextutil.Merge(o.rootCtx, ctx)
	defer cancel()

	return o.repository.DeleteProviderAsset(mergedCtx, providerCode, assetCode)
}
