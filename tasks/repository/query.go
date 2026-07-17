package repository

import (
	"context"

	sqlwrap "github.com/elum2b/services/internal/utils/sql"
)

func repositoryQuery[T any](
	ctx context.Context,
	r *Repository,
	params sqlwrap.Params,
	loader func(ctx context.Context) (T, error),
) (T, error) {
	if params.Timeout <= 0 && r != nil {
		params.Timeout = r.queryTimeout
	}
	return sqlwrap.Query(ctx, r.db, params, loader)
}

func repositoryValue[T any](
	ctx context.Context,
	r *Repository,
	loader func(ctx context.Context) (T, error),
) (T, error) {
	return repositoryQuery(ctx, r, sqlwrap.Params{}, loader)
}

func repositoryExec(
	ctx context.Context,
	r *Repository,
	loader func(ctx context.Context) error,
) error {
	return sqlwrap.Exec(ctx, r.db, sqlwrap.Params{Timeout: r.queryTimeout}, loader)
}
