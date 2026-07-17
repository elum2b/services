package user

import (
	"context"
)

func (u *User) GetProduct(ctx context.Context, params GetProductParams) (*ProductModel, error) {
	if u == nil || u.products == nil {
		return nil, ErrProductNotInitialized
	}
	return u.products.Get(ctx, params)
}

func (u *User) GetProductByKey(ctx context.Context, params GetProductByKeyParams) (*ProductModel, error) {
	if u == nil || u.products == nil {
		return nil, ErrProductNotInitialized
	}
	return u.products.GetByKey(ctx, params)
}
