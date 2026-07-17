package product

import (
	"context"

	"github.com/elum2b/services/payment/repository"
)

type GetByKeyParams struct {
	Key       string
	AssetCode string
	Locale    string
}

func (a *Product) GetByKey(ctx context.Context, params GetByKeyParams) (*ProductModel, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx

	product, err := a.repository.GetProductByKey(ctx, repository.ProductGetByKeyParams{
		Key:       params.Key,
		AssetCode: params.AssetCode,
		Locale:    params.Locale,
	})
	if err != nil {
		return nil, err
	}

	return mapProduct(product), nil
}
