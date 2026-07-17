package operational

import (
	"context"

	"github.com/elum2b/services/internal/utils/contextutil"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	"github.com/elum2b/services/payment/repository"
	"github.com/elum2b/services/payment/service/checkout"
)

type Operational struct {
	checkout   *checkout.Checkout
	repository *repository.PaymentRepository
	rootCtx    context.Context
}

func New(
	ctx context.Context,
	db *sqlwrap.Client,
	options repository.Options,
	checkoutService *checkout.Checkout,
) *Operational {
	return &Operational{
		checkout:   checkoutService,
		repository: repository.NewPaymentRepositoryWithOptions(db, options),
		rootCtx:    contextutil.Normalize(ctx),
	}
}

func (o *Operational) CreateEvent(ctx context.Context, params CreateEventParams) (uint64, error) {
	mergedCtx, cancel := contextutil.Merge(o.rootCtx, ctx)
	defer cancel()

	return o.checkout.CreateEvent(mergedCtx, params)
}

func (o *Operational) CompleteAttempt(
	ctx context.Context,
	params CompleteAttemptParams,
) (*CompleteAttemptResult, error) {
	mergedCtx, cancel := contextutil.Merge(o.rootCtx, ctx)
	defer cancel()

	return o.checkout.CompleteAttempt(mergedCtx, params)
}

func (o *Operational) Close() error {
	if o == nil || o.repository == nil {
		return nil
	}

	return o.repository.Close()
}
