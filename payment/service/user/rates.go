package user

import (
	"context"
)

func (u *User) GetUSDTPrice(ctx context.Context, params GetUSDTPriceParams) (*USDTPriceModel, error) {
	if u == nil || u.assets == nil {
		return nil, ErrAssetNotInitialized
	}
	return u.assets.GetUSDTPrice(ctx, params.AssetCode)
}

func (u *User) ListUSDTPrices(ctx context.Context, _ ListUSDTPricesParams) ([]USDTPriceModel, error) {
	if u == nil || u.assets == nil {
		return nil, ErrAssetNotInitialized
	}
	return u.assets.ListUSDTPrices(ctx)
}
