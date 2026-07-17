package user

import (
	"context"
	"time"

	"github.com/elum2b/services/calendar/repository"
	"github.com/elum2b/services/internal/utils/contextutil"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
)

type User struct {
	repository *repository.Repository
	rootCtx    context.Context
}

func New(ctx context.Context, db *sqlwrap.Client) *User {
	return NewWithOptions(ctx, db, 0)
}

func NewWithOptions(ctx context.Context, db *sqlwrap.Client, queryTimeout time.Duration) *User {
	return NewWithRepositoryOptions(ctx, db, repository.Options{QueryTimeout: queryTimeout})
}

func NewWithRepositoryOptions(ctx context.Context, db *sqlwrap.Client, options repository.Options) *User {
	repo, err := repository.NewPreparedWithOptions(context.Background(), db, options)
	if err != nil {
		repo = repository.NewWithOptions(db, options)
	}
	return &User{repository: repo, rootCtx: contextutil.Normalize(ctx)}
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
