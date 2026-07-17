package admin

import (
	"context"

	"github.com/elum2b/services/payment/service/product"
	"github.com/elum2b/services/payment/service/refund"
)

type CreateProductKeyParams = product.CreateKeyParams
type ExecuteRefundParams = refund.Params
type ExecuteRefundResult = refund.Result

func (a *Admin) CreateProductKey(ctx context.Context, params CreateProductKeyParams) (string, error) {
	if a == nil || a.products == nil {
		return "", ErrProductServiceNotInitialized
	}
	return a.products.CreateKey(ctx, params)
}

func (a *Admin) RebuildProductCache(ctx context.Context, workspaceID string) error {
	if a == nil || a.products == nil {
		return ErrProductServiceNotInitialized
	}
	return a.products.RebuildWorkspaceCache(ctx, workspaceID)
}

func (a *Admin) ExecuteRefund(ctx context.Context, params ExecuteRefundParams) (*ExecuteRefundResult, error) {
	if a == nil || a.refunds == nil {
		return nil, ErrRefundServiceNotInitialized
	}
	return a.refunds.Execute(ctx, params)
}
