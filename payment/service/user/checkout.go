package user

import (
	"context"
)

func (u *User) CreateOrder(ctx context.Context, params CreateOrderParams) (*OrderModel, error) {
	if u == nil || u.checkout == nil {
		return nil, ErrCheckoutNotInitialized
	}
	return u.checkout.CreateOrder(ctx, params)
}

func (u *User) CreateOrderByKey(ctx context.Context, params CreateOrderByKeyParams) (*OrderModel, error) {
	if u == nil || u.checkout == nil {
		return nil, ErrCheckoutNotInitialized
	}
	return u.checkout.CreateOrderByKey(ctx, params)
}

func (u *User) CreateAttempt(ctx context.Context, params CreateAttemptParams) (*AttemptModel, error) {
	if u == nil || u.checkout == nil {
		return nil, ErrCheckoutNotInitialized
	}
	return u.checkout.CreateAttempt(ctx, params)
}
