package product

import (
	"context"

	services "github.com/elum2b/services"
	"github.com/elum2b/services/payment/repository"
)

type GetParams struct {
	Identity  services.Identity
	ProductID string
	AssetCode string
	Locale    string
}

func (a *Product) Get(ctx context.Context, params GetParams) (*ProductModel, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()

	if err := params.Identity.Validate(); err != nil {
		return nil, err
	}

	product, err := a.repository.GetProduct(mergedCtx, repository.ProductGetParams{
		AppID:          params.Identity.AppID,
		WorkspaceID:    params.Identity.WorkspaceID,
		PlatformID:     params.Identity.PlatformID,
		Platform:       params.Identity.Platform,
		PlatformUserID: params.Identity.PlatformUserID,
		IsPremium:      params.Identity.IsPremium,
		Sex:            params.Identity.Sex,
		Country:        params.Identity.Country,
		ProductID:      params.ProductID,
		AssetCode:      params.AssetCode,
		Locale:         params.Locale,
	})
	if err != nil {
		return nil, err
	}

	return mapProduct(product), nil
}
