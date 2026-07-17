package user

import (
	"context"
	"time"

	"github.com/elum2b/services/internal/utils/contextutil"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	"github.com/elum2b/services/reference/repository"
)

type User struct {
	repository *repository.Repository
	rootCtx    context.Context
}

func NewWithRepositoryOptions(ctx context.Context, db *sqlwrap.Client, options repository.Options) *User {
	repo, err := repository.NewPreparedWithOptions(contextutil.Normalize(ctx), db, options)
	if err != nil {
		repo = repository.NewWithOptions(db, options)
	}
	return &User{repository: repo, rootCtx: contextutil.Normalize(ctx)}
}

func New(ctx context.Context, db *sqlwrap.Client) *User {
	return NewWithRepositoryOptions(ctx, db, repository.Options{
		CacheL1Delay: 10 * time.Minute, CacheL2Delay: 10 * time.Minute,
	})
}

func (u *User) Close() error {
	if u == nil || u.repository == nil {
		return nil
	}
	return u.repository.Close()
}

func (u *User) withContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return contextutil.Merge(u.rootCtx, ctx)
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
