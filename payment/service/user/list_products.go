package user

import (
	"context"
)

func (u *User) ListProducts(ctx context.Context, params ListProductsParams) ([]ProductModel, error) {
	if u == nil || u.products == nil {
		return nil, ErrProductNotInitialized
	}
	return u.products.List(ctx, params)
}
