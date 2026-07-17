package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	services "github.com/elum2b/services"
	callbackutil "github.com/elum2b/services/internal/utils/callback"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	tasksqlc "github.com/elum2b/services/tasks/sqlc"
)

type Repository struct {
	db                       *sqlwrap.Client
	q                        *tasksqlc.Queries
	callbacks                *callbackutil.Store
	executor                 tasksqlc.DBTX
	queryTimeout             time.Duration
	cacheL1Delay             time.Duration
	cacheL2Delay             time.Duration
	onCacheInvalidationError func(error)
}

const DefaultQueryTimeout = time.Second
const bootstrapQueryTimeout = 30 * time.Second

func requireWorkspaceID(workspaceID string) error {
	return services.ValidateWorkspaceID(workspaceID)
}

type Options struct {
	QueryTimeout             time.Duration
	CacheL1Delay             time.Duration
	CacheL2Delay             time.Duration
	OnCacheInvalidationError func(error)
}

func New(db *sqlwrap.Client) *Repository {
	return NewWithOptions(db, Options{
		CacheL1Delay: 10 * time.Minute,
		CacheL2Delay: 10 * time.Minute,
	})
}

func NewWithOptions(db *sqlwrap.Client, options Options) *Repository {
	queryTimeout := options.QueryTimeout
	if queryTimeout <= 0 {
		queryTimeout = DefaultQueryTimeout
	}
	executor := db.WithQueryTimeout(queryTimeout)
	return &Repository{
		db:                       db,
		q:                        tasksqlc.New(executor),
		callbacks:                callbackutil.NewWithTable(db.DB(), callbackutil.TasksTable),
		executor:                 executor,
		queryTimeout:             queryTimeout,
		cacheL1Delay:             options.CacheL1Delay,
		cacheL2Delay:             options.CacheL2Delay,
		onCacheInvalidationError: options.OnCacheInvalidationError,
	}
}

func NewPrepared(ctx context.Context, db *sqlwrap.Client) (*Repository, error) {
	return NewPreparedWithOptions(ctx, db, Options{})
}

func NewPreparedWithOptions(_ context.Context, db *sqlwrap.Client, options Options) (*Repository, error) {
	queryTimeout := options.QueryTimeout
	if queryTimeout <= 0 {
		queryTimeout = DefaultQueryTimeout
	}
	executor := db.WithQueryTimeout(queryTimeout)
	return &Repository{
		db:                       db,
		q:                        tasksqlc.New(executor),
		callbacks:                callbackutil.NewWithTable(db.DB(), callbackutil.TasksTable),
		executor:                 executor,
		queryTimeout:             queryTimeout,
		cacheL1Delay:             options.CacheL1Delay,
		cacheL2Delay:             options.CacheL2Delay,
		onCacheInvalidationError: options.OnCacheInvalidationError,
	}, nil
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
	_, err := sqlwrap.Transaction(
		ctx,
		r.db,
		sqlwrap.Params{Timeout: r.queryTimeout},
		func(ctx context.Context, tx *sql.Tx) (struct{}, error) {
			txRepo := &Repository{
				db:                       r.db,
				q:                        r.q.WithTx(tx),
				callbacks:                r.callbacks.WithTx(tx),
				executor:                 tx,
				queryTimeout:             r.queryTimeout,
				cacheL1Delay:             r.cacheL1Delay,
				cacheL2Delay:             r.cacheL2Delay,
				onCacheInvalidationError: r.onCacheInvalidationError,
			}
			return struct{}{}, fn(txRepo)
		},
	)
	return err
}

func (r *Repository) lockWorkspaceMutation(ctx context.Context, workspaceID string) error {
	if err := services.ValidateWorkspaceID(workspaceID); err != nil {
		return err
	}

	_, err := r.executor.ExecContext(
		ctx,
		"SELECT pg_advisory_xact_lock(hashtextextended($1, 0))",
		"tasks:"+workspaceID,
	)
	return err
}

func (r *Repository) withWorkspaceMutation(
	ctx context.Context,
	workspaceID string,
	fn func(*Repository) error,
) error {
	return r.WithTx(ctx, func(txRepo *Repository) error {
		if err := txRepo.lockWorkspaceMutation(ctx, workspaceID); err != nil {
			return err
		}

		return fn(txRepo)
	})
}

func (r *Repository) Bootstrap(ctx context.Context) error {
	if err := r.applySQL(ctx, tasksqlc.SchemaSQL, "schema"); err != nil {
		return err
	}
	if err := r.applySchemaUpgrades(ctx); err != nil {
		return err
	}
	if err := sqlwrap.Exec(ctx, r.db, sqlwrap.Params{Timeout: bootstrapQueryTimeout}, func(ctx context.Context) error {
		return callbackutil.BootstrapTable(ctx, r.db.DB(), callbackutil.TasksTable)
	}); err != nil {
		return err
	}
	if err := r.applySQL(ctx, tasksqlc.TriggerSQL, "trigger"); err != nil {
		return err
	}
	return r.applySQL(ctx, tasksqlc.EventSQL, "event")
}

func (r *Repository) applySchemaUpgrades(ctx context.Context) error {
	upgrades := []struct {
		name string
		sql  string
	}{
		{
			name: "task_definition.action_kind",
			sql:  "ALTER TABLE task_definition ALTER COLUMN action_kind TYPE VARCHAR(64)",
		},
		{
			name: "task_reward.scale",
			sql:  "ALTER TABLE task_reward ADD COLUMN IF NOT EXISTS scale SMALLINT NOT NULL DEFAULT 0",
		},
		{
			name: "task_partner_reward_rule.scale",
			sql:  "ALTER TABLE task_partner_reward_rule ADD COLUMN IF NOT EXISTS scale SMALLINT NOT NULL DEFAULT 0",
		},
		{
			name: "task_partner_config.webhook_secret",
			sql:  "ALTER TABLE task_partner_config ADD COLUMN IF NOT EXISTS webhook_secret VARCHAR(128) NULL",
		},
		{
			name: "task_definition.start_mode",
			sql:  "ALTER TABLE task_definition ADD COLUMN IF NOT EXISTS start_mode VARCHAR(64) NOT NULL DEFAULT 'none'",
		},
		{
			name: "task_partner_issue.external_click_id",
			sql:  "ALTER TABLE task_partner_issue ADD COLUMN IF NOT EXISTS external_click_id VARCHAR(255) NULL",
		},
		{
			name: "task_partner_issue.start_mode",
			sql:  "ALTER TABLE task_partner_issue ADD COLUMN IF NOT EXISTS start_mode VARCHAR(64) NOT NULL DEFAULT 'none'",
		},
		{
			name: "task_partner_issue.started_at",
			sql:  "ALTER TABLE task_partner_issue ADD COLUMN IF NOT EXISTS started_at TIMESTAMPTZ NULL",
		},
		{
			name: "task_partner_stats_daily.revoked_count",
			sql:  "ALTER TABLE task_partner_stats_daily ADD COLUMN IF NOT EXISTS revoked_count BIGINT NOT NULL DEFAULT 0",
		},
		{
			name: "task_partner_stats_daily.revoked_after_claim_count",
			sql:  "ALTER TABLE task_partner_stats_daily ADD COLUMN IF NOT EXISTS revoked_after_claim_count BIGINT NOT NULL DEFAULT 0",
		},
	}
	for _, upgrade := range upgrades {
		if err := sqlwrap.Exec(ctx, r.db, sqlwrap.Params{Timeout: bootstrapQueryTimeout}, func(ctx context.Context) error {
			_, err := r.db.DB().ExecContext(ctx, upgrade.sql)
			return err
		}); err != nil {
			return fmt.Errorf("tasks schema upgrade %s failed: %w", upgrade.name, err)
		}
	}
	return nil
}

func (r *Repository) applySQL(ctx context.Context, raw, source string) error {
	statements, err := sqlwrap.SplitStatements(raw)
	if err != nil {
		return fmt.Errorf("tasks %s SQL parse failed: %w", source, err)
	}
	for _, statement := range statements {
		if err := sqlwrap.Exec(ctx, r.db, sqlwrap.Params{Timeout: bootstrapQueryTimeout}, func(ctx context.Context) error {
			_, err := r.db.DB().ExecContext(ctx, statement)
			return err
		}); err != nil {
			return fmt.Errorf("tasks %s SQL statement failed: %w\n%s", source, err, statement)
		}
	}
	return nil
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

func isNoRows(err error) bool { return errors.Is(err, sql.ErrNoRows) }
