package checkout

import (
	"context"

	"github.com/elum2b/services/payment/repository"
)

type PurchaseWithBalanceParams = repository.PurchaseWithBalanceParams
type GetBalancePurchaseParams = repository.GetBalancePurchaseParams
type BalancePurchase = repository.BalancePurchase

func (a *Checkout) PurchaseWithBalance(ctx context.Context, params PurchaseWithBalanceParams) (BalancePurchase, error) {
	ctx, cancel := a.withContext(ctx)
	defer cancel()
	return a.repository.PurchaseWithBalance(ctx, params)
}

func (a *Checkout) GetBalancePurchase(ctx context.Context, params GetBalancePurchaseParams) (BalancePurchase, error) {
	ctx, cancel := a.withContext(ctx)
	defer cancel()
	return a.repository.GetBalancePurchase(ctx, params)
}
