package product

import (
	"context"

	"github.com/elum2b/services/payment/repository"
)

func (a *Product) List(ctx context.Context, params ListParams) ([]ProductModel, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	if err := params.Identity.Validate(); err != nil {
		return nil, err
	}

	products, err := a.repository.ListProducts(mergedCtx, repository.ProductListParams{
		WorkspaceID:    params.Identity.WorkspaceID,
		AppID:          params.Identity.AppID,
		PlatformID:     params.Identity.PlatformID,
		Platform:       params.Identity.Platform,
		PlatformUserID: params.Identity.PlatformUserID,
		IsPremium:      params.Identity.IsPremium,
		Sex:            params.Identity.Sex,
		Country:        params.Identity.Country,
		GroupCode:      params.GroupCode,
		AssetCode:      params.AssetCode,
		Locale:         params.Locale,
	})
	if err != nil {
		return nil, err
	}

	result := make([]ProductModel, 0, len(products))
	for _, item := range products {
		result = append(result, *mapProduct(item))
	}
	return result, nil
}
