package asset

import (
	"context"

	"github.com/elum2b/services/internal/utils/contextutil"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"

	"github.com/elum2b/services/payment/repository"
)

type Asset struct {
	repository *repository.PaymentRepository
	rootCtx    context.Context
}

func New(ctx context.Context, db *sqlwrap.Client) *Asset {
	return NewWithOptions(ctx, db, repository.Options{})
}

func NewWithOptions(ctx context.Context, db *sqlwrap.Client, options repository.Options) *Asset {
	repo, err := repository.NewPreparedPaymentRepositoryWithOptions(context.Background(), db, options)
	if err == nil {
		return &Asset{repository: repo, rootCtx: contextutil.Normalize(ctx)}
	}
	return &Asset{
		repository: repository.NewPaymentRepositoryWithOptions(db, options),
		rootCtx:    contextutil.Normalize(ctx),
	}
}

func (a *Asset) Close() error {
	if a == nil || a.repository == nil {
		return nil
	}
	return a.repository.Close()
}

func (a *Asset) withContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return contextutil.Merge(a.rootCtx, ctx)
}
