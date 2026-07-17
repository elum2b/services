package admin

import (
	"context"
	"errors"
	"time"

	"github.com/elum2b/services/calendar/repository"
	callbackutil "github.com/elum2b/services/internal/utils/callback"
	"github.com/elum2b/services/internal/utils/contextutil"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
)

type Admin struct {
	repository *repository.Repository
	callbacks  *callbackutil.Store
	rootCtx    context.Context
}

func New(ctx context.Context, db *sqlwrap.Client) *Admin {
	return NewWithOptions(ctx, db, 0)
}

func NewWithOptions(ctx context.Context, db *sqlwrap.Client, queryTimeout time.Duration) *Admin {
	return NewWithRepositoryOptions(ctx, db, repository.Options{QueryTimeout: queryTimeout})
}

func NewWithRepositoryOptions(ctx context.Context, db *sqlwrap.Client, options repository.Options) *Admin {
	repo, err := repository.NewPreparedWithOptions(context.Background(), db, options)
	if err != nil {
		repo = repository.NewWithOptions(db, options)
	}
	return &Admin{
		repository: repo,
		callbacks:  callbackutil.NewWithTable(db.DB(), callbackutil.CalendarTable),
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

func normalizePage(page Page) (int32, int32) {
	if page.Limit <= 0 {
		page.Limit = 100
	}
	if page.Limit > 1000 {
		page.Limit = 1000
	}
	if page.Offset < 0 {
		page.Offset = 0
	}
	return page.Limit, page.Offset
}
