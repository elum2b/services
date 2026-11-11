package user

import (
	"context"

	"github.com/elum2b/services/payment/service/checkout"
)

type PurchaseWithBalanceParams = checkout.PurchaseWithBalanceParams
type GetBalancePurchaseParams = checkout.GetBalancePurchaseParams
type BalancePurchase = checkout.BalancePurchase

func (u *User) PurchaseWithBalance(ctx context.Context, params PurchaseWithBalanceParams) (BalancePurchase, error) {
	if u == nil || u.checkout == nil {
		return BalancePurchase{}, ErrCheckoutNotInitialized
	}
	return u.checkout.PurchaseWithBalance(ctx, params)
}

func (u *User) GetBalancePurchase(ctx context.Context, params GetBalancePurchaseParams) (BalancePurchase, error) {
	if u == nil || u.checkout == nil {
		return BalancePurchase{}, ErrCheckoutNotInitialized
	}
	return u.checkout.GetBalancePurchase(ctx, params)
}
