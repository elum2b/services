package asset

import (
	"context"

	"github.com/elum2b/services/payment/repository"
)

func (a *Asset) Upsert(ctx context.Context, params UpsertParams) error {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx

	return a.repository.UpsertAsset(ctx, repository.AssetUpsertParams{
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
