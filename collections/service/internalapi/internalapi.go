package internalapi

import (
	"context"

	"github.com/elum2b/services/collections/repository"

	"github.com/elum2b/services/internal/utils/contextutil"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
)

type Internal struct {
	repository *repository.Repository
	rootCtx    context.Context
}

func NewWithRepositoryOptions(ctx context.Context, db *sqlwrap.Client, options repository.Options) *Internal {
	return &Internal{
		repository: repository.NewWithOptions(db, options),
		rootCtx:    contextutil.Normalize(ctx),
	}
}
func (s *Internal) Close() error { return nil }
func (s *Internal) withContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return contextutil.Merge(s.rootCtx, ctx)
}
