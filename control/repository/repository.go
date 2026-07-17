package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	services "github.com/elum2b/services"
	controlsqlc "github.com/elum2b/services/control/sqlc"
	serviceerrors "github.com/elum2b/services/errors"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
)

var (
	ErrNotFound     = serviceerrors.New(serviceerrors.CodeNotFound, "control entity not found")
	ErrInvalidScope = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"control workspace or account is required",
	)
	ErrForbidden         = serviceerrors.New(serviceerrors.CodeForbidden, "control access denied")
	ErrRoleHierarchy     = serviceerrors.New(serviceerrors.CodeForbidden, "control role hierarchy denied")
	ErrMethodNotFound    = serviceerrors.New(serviceerrors.CodeNotFound, "control method not found")
	ErrMethodOwner       = serviceerrors.New(serviceerrors.CodeConflict, "control method belongs to another service")
	ErrRoleNotFound      = serviceerrors.New(serviceerrors.CodeNotFound, "control role not found")
	ErrAccountNotFound   = serviceerrors.New(serviceerrors.CodeNotFound, "control account not found")
	ErrWorkspaceNotFound = serviceerrors.New(serviceerrors.CodeNotFound, "control workspace not found")
	ErrInviteMaxUses     = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"control invite max uses must be between 1 and 2147483647",
	)
	ErrTwoFactorEnabled = serviceerrors.New(
		serviceerrors.CodeConflict,
		"control two-factor authentication is already enabled",
	)
	ErrSecretEncryptionKey = serviceerrors.New(
		serviceerrors.CodeNotReady,
		"control secret encryption key must contain 32 bytes",
	)
)

const bootstrapQueryTimeout = 30 * time.Second

type Options struct {
	QueryTimeout             time.Duration
	CacheL1Delay             time.Duration
	CacheL2Delay             time.Duration
	OnCacheInvalidationError func(error)
	SecretEncryptionKey      []byte
}

type Repository struct {
	db                       *sqlwrap.Client
	q                        *controlsqlc.Queries
	timeout                  time.Duration
	cacheL1                  time.Duration
	cacheL2                  time.Duration
	onCacheInvalidationError func(error)
	secretEncryptionKey      []byte
}

func New(db *sqlwrap.Client) *Repository { return NewWithOptions(db, Options{}) }

func NewWithOptions(db *sqlwrap.Client, options Options) *Repository {
	timeout := options.QueryTimeout
	if timeout <= 0 {
		timeout = time.Second
	}
	cacheL1, cacheL2 := options.CacheL1Delay, options.CacheL2Delay
	if cacheL1 <= 0 {
		cacheL1 = time.Second
	}
	if cacheL2 <= 0 {
		cacheL2 = time.Second
	}
	return &Repository{
		db:                       db,
		q:                        controlsqlc.New(db.WithQueryTimeout(timeout)),
		timeout:                  timeout,
		cacheL1:                  cacheL1,
		cacheL2:                  cacheL2,
		onCacheInvalidationError: options.OnCacheInvalidationError,
		secretEncryptionKey:      append([]byte(nil), options.SecretEncryptionKey...),
	}
}

func (r *Repository) Close() error {
	if r == nil || r.q == nil {
		return nil
	}
	return r.q.Close()
}

func (r *Repository) Bootstrap(ctx context.Context) error {
	if err := r.execBootstrapSQL(ctx, controlsqlc.SchemaSQL, "schema"); err != nil {
		return err
	}
	if err := r.execBootstrapSQL(ctx, controlsqlc.CatalogSQL, "catalog"); err != nil {
		return err
	}
	r.bumpCacheVersion("control", "access-catalog")
	return nil
}

func (r *Repository) bumpCacheVersion(parts ...any) {
	if r == nil || r.db == nil {
		return
	}
	if err := r.db.BumpCacheVersion(parts...); err != nil && r.onCacheInvalidationError != nil {
		func() {
			defer func() {
				_ = recover()
			}()
			r.onCacheInvalidationError(err)
		}()
	}
}

func (r *Repository) execBootstrapSQL(ctx context.Context, raw, name string) error {
	statements, err := sqlwrap.SplitStatements(raw)
	if err != nil {
		return fmt.Errorf("control %s SQL parse failed: %w", name, err)
	}

	for _, statement := range statements {
		if err := sqlwrap.Exec(ctx, r.db, sqlwrap.Params{Timeout: bootstrapQueryTimeout}, func(ctx context.Context) error {
			_, err := r.db.DB().ExecContext(ctx, statement)
			return err
		}); err != nil {
			return fmt.Errorf("control %s statement failed: %w\n%s", name, err, statement)
		}
	}
	return nil
}

func normalizeID(value string) string { return strings.TrimSpace(value) }

func requireWorkspaceID(workspaceID string) error {
	return services.ValidateWorkspaceID(workspaceID)
}

func required(values ...string) error {
	for _, value := range values {
		if normalizeID(value) == "" {
			return ErrInvalidScope
		}
	}
	return nil
}

func noRows(err error, fallback error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fallback
	}
	return err
}
