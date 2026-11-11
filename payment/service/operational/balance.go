package operational

import (
	"context"

	"github.com/elum2b/services/internal/utils/contextutil"
	"github.com/elum2b/services/payment/repository"
)

type BalanceDeliveryParams = repository.BalanceDeliveryParams
type BalanceDelivery = repository.BalanceDelivery

func (o *Operational) GetBalanceDelivery(ctx context.Context, params BalanceDeliveryParams) (BalanceDelivery, error) {
	ctx, cancel := contextutil.Merge(o.rootCtx, ctx)
	defer cancel()
	return o.repository.GetBalanceDelivery(ctx, params)
}

func (o *Operational) RetryBalanceDelivery(ctx context.Context, params BalanceDeliveryParams) (bool, error) {
	ctx, cancel := contextutil.Merge(o.rootCtx, ctx)
	defer cancel()
	return o.repository.RetryBalanceDelivery(ctx, params)
}
