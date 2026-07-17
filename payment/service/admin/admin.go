package admin

import (
	"context"
	"errors"

	callbackutil "github.com/elum2b/services/internal/utils/callback"
	"github.com/elum2b/services/internal/utils/contextutil"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	"github.com/elum2b/services/payment/repository"
	"github.com/elum2b/services/payment/service/product"
	"github.com/elum2b/services/payment/service/refund"
)

type Admin struct {
	repository *repository.PaymentRepository
	callbacks  *callbackutil.Store
	products   *product.Product
	refunds    *refund.Refund
	rootCtx    context.Context
}

func New(ctx context.Context, db *sqlwrap.Client) *Admin {
	return NewWithOptions(ctx, db, repository.Options{})
}

func NewWithOptions(ctx context.Context, db *sqlwrap.Client, options repository.Options) *Admin {
	return NewWithServices(ctx, db, options, nil, nil)
}

func NewWithServices(
	ctx context.Context,
	db *sqlwrap.Client,
	options repository.Options,
	products *product.Product,
	refunds *refund.Refund,
) *Admin {
	return &Admin{
		repository: repository.NewPaymentRepositoryWithOptions(db, options),
		callbacks:  callbackutil.NewWithTable(db.DB(), callbackutil.PaymentTable),
		products:   products,
		refunds:    refunds,
		rootCtx:    contextutil.Normalize(ctx),
	}
}

func (a *Admin) Close() error {
	if a == nil {
		return nil
	}
	var err error
	if a.repository != nil {
		err = errors.Join(err, a.repository.Close())
	}
	if a.callbacks != nil {
		err = errors.Join(err, a.callbacks.Close())
	}
	return err
}

func (a *Admin) withContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return contextutil.Merge(a.rootCtx, ctx)
}
