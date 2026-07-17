package admin

import (
	"context"

	"github.com/elum2b/services/internal/utils/contextutil"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	"github.com/elum2b/services/tasks/repository"
)

type Admin struct {
	rootCtx    context.Context
	repository *repository.Repository
}

func New(ctx context.Context, db *sqlwrap.Client) *Admin {
	return &Admin{rootCtx: contextutil.Normalize(ctx), repository: repository.New(db)}
}

func NewWithOptions(ctx context.Context, db *sqlwrap.Client, options repository.Options) *Admin {
	return &Admin{rootCtx: contextutil.Normalize(ctx), repository: repository.NewWithOptions(db, options)}
}

func (a *Admin) Close() error {
	if a == nil || a.repository == nil {
		return nil
	}
	return a.repository.Close()
}

func (a *Admin) withContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if a == nil {
		return contextutil.Merge(context.Background(), ctx)
	}
	return contextutil.Merge(a.rootCtx, ctx)
}

func (a *Admin) Bootstrap(ctx context.Context) error {
	if a == nil || a.repository == nil {
		return ErrRepositoryNotConfigured
	}
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	return a.repository.Bootstrap(mergedCtx)
}
