package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	services "github.com/elum2b/services"
	serviceerrors "github.com/elum2b/services/errors"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	refsqlc "github.com/elum2b/services/reference/sqlc"
)

var (
	ErrWorkspaceRequired = serviceerrors.New(serviceerrors.CodeInvalidFields, "reference workspace is required")
	ErrItemNotFound      = serviceerrors.New(serviceerrors.CodeNotFound, "reference item not found")
)

const bootstrapQueryTimeout = 30 * time.Second

type Options struct {
	QueryTimeout             time.Duration
	CacheL1Delay             time.Duration
	CacheL2Delay             time.Duration
	OnCacheInvalidationError func(error)
}

type Repository struct {
	db                       *sqlwrap.Client
	q                        *refsqlc.Queries
	executor                 refsqlc.DBTX
	timeout                  time.Duration
	cacheL1                  time.Duration
	cacheL2                  time.Duration
	onCacheInvalidationError func(error)
}

func New(db *sqlwrap.Client) *Repository {
	return NewWithOptions(db, Options{
		CacheL1Delay: 10 * time.Minute,
		CacheL2Delay: 10 * time.Minute,
	})
}

func NewWithOptions(db *sqlwrap.Client, options Options) *Repository {
	timeout := options.QueryTimeout
	if timeout <= 0 {
		timeout = time.Second
	}
	executor := db.WithQueryTimeout(timeout)
	return &Repository{
		db:                       db,
		q:                        refsqlc.New(executor),
		executor:                 executor,
		timeout:                  timeout,
		cacheL1:                  options.CacheL1Delay,
		cacheL2:                  options.CacheL2Delay,
		onCacheInvalidationError: options.OnCacheInvalidationError,
	}
}

func NewPreparedWithOptions(ctx context.Context, db *sqlwrap.Client, options Options) (*Repository, error) {
	repository := NewWithOptions(db, options)
	q, err := refsqlc.Prepare(ctx, db.WithQueryTimeout(repository.timeout))
	if err != nil {
		return nil, err
	}
	repository.q = q
	repository.executor = db.WithQueryTimeout(repository.timeout)
	return repository, nil
}

func (r *Repository) Close() error {
	if r == nil || r.q == nil {
		return nil
	}
	return r.q.Close()
}

func (r *Repository) WithTx(ctx context.Context, fn func(*Repository) error) error {
	_, err := sqlwrap.Transaction(
		ctx,
		r.db,
		sqlwrap.Params{Timeout: r.timeout},
		func(ctx context.Context, tx *sql.Tx) (struct{}, error) {
			txRepo := &Repository{
				db:                       r.db,
				q:                        r.q.WithTx(tx),
				executor:                 tx,
				timeout:                  r.timeout,
				cacheL1:                  r.cacheL1,
				cacheL2:                  r.cacheL2,
				onCacheInvalidationError: r.onCacheInvalidationError,
			}
			return struct{}{}, fn(txRepo)
		},
	)
	return err
}

func (r *Repository) Bootstrap(ctx context.Context) error {
	statements, err := sqlwrap.SplitStatements(refsqlc.SchemaSQL)
	if err != nil {
		return fmt.Errorf("reference schema SQL parse failed: %w", err)
	}
	for _, statement := range statements {
		if err := sqlwrap.Exec(ctx, r.db, sqlwrap.Params{Timeout: bootstrapQueryTimeout}, func(ctx context.Context) error {
			_, err := r.db.DB().ExecContext(ctx, statement)
			return err
		}); err != nil {
			return fmt.Errorf("reference schema statement failed: %w\n%s", err, statement)
		}
	}
	statements, err = sqlwrap.SplitStatements(refsqlc.TriggerSQL)
	if err != nil {
		return fmt.Errorf("reference trigger SQL parse failed: %w", err)
	}
	for _, statement := range statements {
		if err := sqlwrap.Exec(ctx, r.db, sqlwrap.Params{Timeout: bootstrapQueryTimeout}, func(ctx context.Context) error {
			_, err := r.db.DB().ExecContext(ctx, statement)
			return err
		}); err != nil {
			return fmt.Errorf("reference trigger statement failed: %w\n%s", err, statement)
		}
	}
	return nil
}

func requireWorkspace(workspaceID string) error {
	return services.ValidateWorkspaceID(workspaceID)
}

func mapNoRows(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrItemNotFound
	}
	return err
}
