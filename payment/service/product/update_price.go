package product

import (
	"context"
	"time"

	"github.com/elum2b/services/payment/repository"
)

type UpdatePriceParams struct {
	ID                           uint64
	WorkspaceID                  string
	AssetCode                    string
	ListAmountMinor              uint64
	DiscountAmountMinor          uint64
	PricingMode                  string
	ReferenceAssetCode           *string
	ReferenceListAmountMinor     *uint64
	ReferenceDiscountAmountMinor *uint64
	Coefficient                  *string
	IsPromotion                  bool
	StartsAt                     *time.Time
	EndsAt                       *time.Time
}

func (a *Product) UpdatePrice(ctx context.Context, params UpdatePriceParams) (int64, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx

	return a.repository.UpdateProductPrice(ctx, repository.ProductPriceUpdateParams{
		ID:                           params.ID,
		WorkspaceID:                  params.WorkspaceID,
		AssetCode:                    params.AssetCode,
		ListAmountMinor:              params.ListAmountMinor,
		DiscountAmountMinor:          params.DiscountAmountMinor,
		PricingMode:                  params.PricingMode,
		ReferenceAssetCode:           params.ReferenceAssetCode,
		ReferenceListAmountMinor:     params.ReferenceListAmountMinor,
		ReferenceDiscountAmountMinor: params.ReferenceDiscountAmountMinor,
		Coefficient:                  params.Coefficient,
		IsPromotion:                  params.IsPromotion,
		StartsAt:                     params.StartsAt,
		EndsAt:                       params.EndsAt,
	})
}
