package admin

import (
	"context"

	"github.com/elum2b/services/payment/service/product"
)

type SaveProductParams = product.UpsertParams
type SaveProductGroupParams = product.UpsertGroupParams
type SaveLocalizationParams = product.UpsertLocalizationParams
type AttachProductItemParams = product.AddItemParams
type CreateCatalogPriceParams = product.CreatePriceParams
type UpdateCatalogPriceParams = product.UpdatePriceParams

func (a *Admin) SaveProduct(ctx context.Context, params SaveProductParams) error {
	if a == nil || a.products == nil {
		return ErrProductServiceNotInitialized
	}
	return a.products.Upsert(ctx, params)
}

func (a *Admin) SaveProductGroup(ctx context.Context, params SaveProductGroupParams) error {
	if a == nil || a.products == nil {
		return ErrProductServiceNotInitialized
	}
	return a.products.UpsertGroup(ctx, params)
}

func (a *Admin) SaveLocalization(ctx context.Context, params SaveLocalizationParams) error {
	if a == nil || a.products == nil {
		return ErrProductServiceNotInitialized
	}
	return a.products.UpsertLocalization(ctx, params)
}

func (a *Admin) AttachProductItem(ctx context.Context, params AttachProductItemParams) error {
	if a == nil || a.products == nil {
		return ErrProductServiceNotInitialized
	}
	return a.products.AddItem(ctx, params)
}

func (a *Admin) CreateCatalogPrice(ctx context.Context, params CreateCatalogPriceParams) (uint64, error) {
	if a == nil || a.products == nil {
		return 0, ErrProductServiceNotInitialized
	}
	return a.products.CreatePrice(ctx, params)
}

func (a *Admin) UpdateCatalogPrice(ctx context.Context, params UpdateCatalogPriceParams) (int64, error) {
	if a == nil || a.products == nil {
		return 0, ErrProductServiceNotInitialized
	}
	return a.products.UpdatePrice(ctx, params)
}
