package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	services "github.com/elum2b/services"
	callbackutil "github.com/elum2b/services/internal/utils/callback"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	promosqlc "github.com/elum2b/services/promo/sqlc"
	"github.com/jackc/pgx/v5/pgconn"
)

func requireWorkspaceID(workspaceID string) error {
	return services.ValidateWorkspaceID(workspaceID)
}

type Repository struct {
	db                       *sqlwrap.Client
	q                        *promosqlc.Queries
	callbacks                *callbackutil.Store
	executor                 promosqlc.DBTX
	timeout                  time.Duration
	cacheL1                  time.Duration
	cacheL2                  time.Duration
	onCacheInvalidationError func(error)
}

type Options struct {
	QueryTimeout             time.Duration
	CacheL1Delay             time.Duration
	CacheL2Delay             time.Duration
	OnCacheInvalidationError func(error)
}

const bootstrapQueryTimeout = 30 * time.Second

func New(db *sqlwrap.Client) *Repository {
	return NewWithOptions(db, Options{
		CacheL1Delay: 10 * time.Minute,
		CacheL2Delay: 10 * time.Minute,
	})
}

func NewWithOptions(db *sqlwrap.Client, options Options) *Repository {
	timeout := queryTimeout(options.QueryTimeout)
	executor := db.WithQueryTimeout(timeout)
	q := promosqlc.New(executor)
	return &Repository{
		db:                       db,
		q:                        q,
		callbacks:                callbackutil.NewWithTable(db.DB(), callbackutil.PromoTable),
		executor:                 executor,
		timeout:                  timeout,
		cacheL1:                  options.CacheL1Delay,
		cacheL2:                  options.CacheL2Delay,
		onCacheInvalidationError: options.OnCacheInvalidationError,
	}
}

func NewPrepared(ctx context.Context, db *sqlwrap.Client) (*Repository, error) {
	return NewPreparedWithOptions(ctx, db, Options{})
}

func NewPreparedWithOptions(_ context.Context, db *sqlwrap.Client, options Options) (*Repository, error) {
	return NewWithOptions(db, options), nil
}

func (r *Repository) Close() error {
	if r == nil {
		return nil
	}
	var err error
	if r.q != nil {
		err = errors.Join(err, r.q.Close())
	}
	if r.callbacks != nil {
		err = errors.Join(err, r.callbacks.Close())
	}
	return err
}

func (r *Repository) WithTx(ctx context.Context, fn func(*Repository) error) error {
	_, err := sqlwrap.Transaction(ctx, r.db, sqlwrap.Params{
		Timeout: r.timeout,
	}, func(ctx context.Context, tx *sql.Tx) (struct{}, error) {
		txRepo := &Repository{
			db:                       r.db,
			q:                        r.q.WithTx(tx),
			callbacks:                r.callbacks.WithTx(tx),
			executor:                 tx,
			timeout:                  r.timeout,
			cacheL1:                  r.cacheL1,
			cacheL2:                  r.cacheL2,
			onCacheInvalidationError: r.onCacheInvalidationError,
		}
		return struct{}{}, fn(txRepo)
	})
	return err
}

func (r *Repository) Bootstrap(ctx context.Context) error {
	if err := r.applySQL(ctx, promosqlc.SchemaSQL, "schema"); err != nil {
		return err
	}
	if err := sqlwrap.Exec(ctx, r.db, sqlwrap.Params{
		Timeout: bootstrapQueryTimeout,
	}, func(ctx context.Context) error {
		return callbackutil.BootstrapTable(ctx, r.db.DB(), callbackutil.PromoTable)
	}); err != nil {
		return err
	}
	if err := r.applySQL(ctx, promosqlc.TriggerSQL, "trigger"); err != nil {
		return err
	}
	return r.applySQL(ctx, promosqlc.EventSQL, "event")
}

func (r *Repository) applySQL(ctx context.Context, raw, source string) error {
	statements, err := sqlwrap.SplitStatements(raw)
	if err != nil {
		return fmt.Errorf("promo %s SQL parse failed: %w", source, err)
	}

	for _, statement := range statements {
		if err := sqlwrap.Exec(ctx, r.db, sqlwrap.Params{
			Timeout: bootstrapQueryTimeout,
		}, func(ctx context.Context) error {
			_, err := r.db.DB().ExecContext(ctx, statement)
			return err
		}); err != nil {
			if isCreateTypeAlreadyExists(statement, err) {
				continue
			}
			return fmt.Errorf("promo %s SQL statement failed: %w\n%s", source, err, statement)
		}
	}
	return nil
}

func isCreateTypeAlreadyExists(statement string, err error) bool {
	var pgErr *pgconn.PgError
	return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(statement)), "CREATE TYPE ") &&
		errors.As(err, &pgErr) &&
		pgErr.Code == "42710"
}

func queryTimeout(value time.Duration) time.Duration {
	if value <= 0 {
		return time.Second
	}
	return value
}

func isNoRows(err error) bool { return errors.Is(err, sql.ErrNoRows) }

func normalizeCode(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}

func normalizePage(limit, offset int32) (int32, int32) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}
