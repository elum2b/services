package subscription

import (
	"context"

	"github.com/elum2b/services/internal/utils/contextutil"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"

	"github.com/elum2b/services/payment/repository"
)

type Subscription struct {
	repository *repository.PaymentRepository
	rootCtx    context.Context
}

func New(ctx context.Context, db *sqlwrap.Client) *Subscription {
	return NewWithOptions(ctx, db, repository.Options{})
}

func NewWithOptions(ctx context.Context, db *sqlwrap.Client, options repository.Options) *Subscription {
	repo, err := repository.NewPreparedPaymentRepositoryWithOptions(context.Background(), db, options)
	if err == nil {
		return &Subscription{repository: repo, rootCtx: contextutil.Normalize(ctx)}
	}
	return &Subscription{
		repository: repository.NewPaymentRepositoryWithOptions(db, options),
		rootCtx:    contextutil.Normalize(ctx),
	}
}

func (a *Subscription) Close() error {
	if a == nil || a.repository == nil {
		return nil
	}
	return a.repository.Close()
}

func (a *Subscription) withContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return contextutil.Merge(a.rootCtx, ctx)
}
