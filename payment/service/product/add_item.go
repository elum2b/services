package product

import (
	"context"

	"github.com/elum2b/services/payment/repository"
)

type AddItemParams struct {
	WorkspaceID  string
	ProductID    string
	ItemID       string
	RewardType   string
	Quantity     int64
	Scale        uint16
	DurationUnit *string
}

func (a *Product) AddItem(ctx context.Context, params AddItemParams) error {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx

	return a.repository.UpsertProductItem(ctx, repository.ProductItemUpsertParams{
		ProductID:    params.ProductID,
		WorkspaceID:  params.WorkspaceID,
		ItemID:       params.ItemID,
		RewardType:   params.RewardType,
		Quantity:     params.Quantity,
		Scale:        params.Scale,
		DurationUnit: params.DurationUnit,
	})
}
