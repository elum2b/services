package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	services "github.com/elum2b/services"
	cpasqlc "github.com/elum2b/services/cpa/sqlc"
	serviceerrors "github.com/elum2b/services/errors"
	callbackutil "github.com/elum2b/services/internal/utils/callback"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
)

var (
	ErrOfferRequired     = serviceerrors.New(serviceerrors.CodeInvalidFields, "cpa offer id is required")
	ErrLocaleRequired    = serviceerrors.New(serviceerrors.CodeInvalidFields, "cpa locale is required")
	ErrRewardKeyRequired = serviceerrors.New(serviceerrors.CodeInvalidFields, "cpa reward key is required")
	ErrCodeRequired      = serviceerrors.New(serviceerrors.CodeInvalidFields, "cpa code is required")
	ErrInvalidDateRange  = serviceerrors.New(serviceerrors.CodeInvalidFields, "cpa date range is invalid")
	ErrNoCodesAvailable  = serviceerrors.New(serviceerrors.CodeUnavailable, "cpa personal codes are not available")
	ErrInvalidCodeConfig = serviceerrors.New(serviceerrors.CodeInvalidFields, "cpa generated code configuration is invalid")
	ErrCodeUploadMode    = serviceerrors.New(serviceerrors.CodeFailedPrecondition, "cpa codes can only be uploaded to personal pool offers")
	ErrOfferInUse        = serviceerrors.New(serviceerrors.CodeFailedPrecondition, "cpa offer has assignments and cannot be deleted")
)

type Repository struct {
	db                       *sqlwrap.Client
	q                        *cpasqlc.Queries
	callbacks                *callbackutil.Store
	executor                 cpasqlc.DBTX
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
	return &Repository{
		db:                       db,
		q:                        cpasqlc.New(executor),
		callbacks:                callbackutil.NewWithTable(db.DB(), callbackutil.CPATable),
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
	_, err := sqlwrap.Transaction(
		ctx,
		r.db,
		sqlwrap.Params{
			Timeout: r.timeout,
		},
		func(ctx context.Context, tx *sql.Tx) (struct{}, error) {
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
		},
	)
	return err
}

func (r *Repository) WithReadOnlySnapshot(ctx context.Context, fn func(*Repository) error) error {
	return r.WithTx(ctx, func(txRepo *Repository) error {
		if _, err := txRepo.executor.ExecContext(
			ctx,
			"SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY",
		); err != nil {
			return err
		}

		return fn(txRepo)
	})
}

func (r *Repository) Bootstrap(ctx context.Context) error {
	if err := r.applySQL(ctx, cpasqlc.SchemaSQL, "schema"); err != nil {
		return err
	}
	if err := r.applySchemaUpgrades(ctx); err != nil {
		return err
	}
	if err := sqlwrap.Exec(
		ctx,
		r.db,
		sqlwrap.Params{
			Timeout: bootstrapQueryTimeout,
		},
		func(ctx context.Context) error {
			return callbackutil.BootstrapTable(ctx, r.db.DB(), callbackutil.CPATable)
		},
	); err != nil {
		return err
	}
	return r.applySQL(ctx, cpasqlc.EventSQL, "event")
}

func (r *Repository) applySchemaUpgrades(ctx context.Context) error {
	return r.applySQL(ctx, `
ALTER TABLE cpa_assignment
    ADD COLUMN IF NOT EXISTS rewards_snapshot JSONB;

UPDATE cpa_assignment assignment
SET rewards_snapshot = COALESCE((
    SELECT jsonb_agg(
        jsonb_build_object(
            'key', reward.reward_key,
            'type', reward.reward_type,
            'quantity', reward.quantity,
            'scale', reward.scale,
            'unit', reward.duration_unit
        )
        ORDER BY reward.id
    )
    FROM cpa_reward reward
    WHERE reward.workspace_id = assignment.workspace_id
      AND reward.cpa_id = assignment.cpa_id
), '[]'::jsonb)
WHERE assignment.rewards_snapshot IS NULL;

ALTER TABLE cpa_assignment
    ALTER COLUMN rewards_snapshot SET DEFAULT '[]'::jsonb,
    ALTER COLUMN rewards_snapshot SET NOT NULL;

ALTER TABLE cpa_reward
    ALTER COLUMN scale TYPE INTEGER;

ALTER TABLE cpa_reward
    DROP CONSTRAINT IF EXISTS cpa_reward_scale_chk;

ALTER TABLE cpa_reward
    ADD CONSTRAINT cpa_reward_scale_chk CHECK (scale BETWEEN 0 AND 65535);
`, "upgrade")
}

func (r *Repository) applySQL(ctx context.Context, raw, source string) error {
	statements, err := sqlwrap.SplitStatements(raw)
	if err != nil {
		return fmt.Errorf("cpa %s SQL parse failed: %w", source, err)
	}
	for _, statement := range statements {
		if err := sqlwrap.Exec(
			ctx,
			r.db,
			sqlwrap.Params{
				Timeout: bootstrapQueryTimeout,
			},
			func(ctx context.Context) error {
				_, err := r.db.DB().ExecContext(ctx, statement)
				return err
			},
		); err != nil {
			if isCreateTypeAlreadyExists(statement, err) {
				continue
			}
			return fmt.Errorf("cpa %s SQL statement failed: %w\n%s", source, err, statement)
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

func requireScope(workspaceID, cpaID string) error {
	if err := requireWorkspace(workspaceID); err != nil {
		return err
	}
	if strings.TrimSpace(cpaID) == "" {
		return ErrOfferRequired
	}
	return validateStoredString("cpa_id", cpaID, maxOfferIDLength)
}

func requireWorkspace(workspaceID string) error {
	return services.ValidateWorkspaceID(workspaceID)
}

func (r *Repository) lockWorkspaceMutation(ctx context.Context, workspaceID string) error {
	_, err := r.executor.ExecContext(
		ctx,
		"SELECT pg_advisory_xact_lock(hashtextextended($1, 0))",
		workspaceID,
	)
	return err
}

func (r *Repository) lockWorkspaceCatalogRead(ctx context.Context, workspaceID string) error {
	_, err := r.executor.ExecContext(
		ctx,
		"SELECT pg_advisory_xact_lock_shared(hashtextextended($1, 0))",
		workspaceID,
	)
	return err
}

func (r *Repository) lockIssueIdentity(ctx context.Context, scope UserScope) error {
	key := fmt.Sprintf(
		"cpa:issue:%s:%s:%d:%d:%s",
		scope.WorkspaceID,
		scope.CPAID,
		scope.AppID,
		scope.PlatformID,
		scope.PlatformUserID,
	)
	_, err := r.executor.ExecContext(
		ctx,
		"SELECT pg_advisory_xact_lock(hashtextextended($1, 0))",
		key,
	)
	return err
}

func requireLocale(locale string) error {
	if strings.TrimSpace(locale) == "" {
		return ErrLocaleRequired
	}
	return validateStoredString("locale", locale, maxLocaleLength)
}

func requireRewardKey(rewardKey string) error {
	if strings.TrimSpace(rewardKey) == "" {
		return ErrRewardKeyRequired
	}
	return validateStoredString("reward_key", rewardKey, maxRewardKeyLength)
}

func isNoRows(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}

func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && (pgErr.Code == "23503" || pgErr.Code == "23001")
}
