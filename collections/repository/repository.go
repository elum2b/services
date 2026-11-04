package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/elum2b/services/collections/model"
	collectionsSQL "github.com/elum2b/services/collections/sqlc"

	services "github.com/elum2b/services"
	serviceerrors "github.com/elum2b/services/errors"
	callbackutil "github.com/elum2b/services/internal/utils/callback"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
)

var (
	ErrInvalid   = serviceerrors.New(serviceerrors.CodeInvalidFields, "collections invalid argument")
	ErrNotFound  = serviceerrors.New(serviceerrors.CodeNotFound, "collections entity not found")
	ErrConflict  = serviceerrors.New(serviceerrors.CodeConflict, "collections operation or revision conflict")
	ErrForbidden = serviceerrors.New(serviceerrors.CodeForbidden, "collections subject scope denied")
)

type Options struct {
	QueryTimeout             time.Duration
	CacheL1Delay             time.Duration
	CacheL2Delay             time.Duration
	CatalogCacheTTL          time.Duration
	OnCacheInvalidationError func(error)
}
type Repository struct {
	db      *sqlwrap.Client
	q       *collectionsSQL.Queries
	options Options
}

func New(db *sqlwrap.Client) *Repository { return NewWithOptions(db, Options{}) }
func NewWithOptions(db *sqlwrap.Client, options Options) *Repository {
	if options.CatalogCacheTTL <= 0 {
		options.CatalogCacheTTL = 24 * time.Hour
	}
	if options.QueryTimeout <= 0 {
		options.QueryTimeout = time.Second
	}
	return &Repository{
		db:      db,
		q:       collectionsSQL.New(db.WithQueryTimeout(options.QueryTimeout)),
		options: options,
	}
}
func (r *Repository) Close() error { return nil }
func (r *Repository) Bootstrap(ctx context.Context) error {
	return sqlwrap.Exec(ctx, r.db, sqlwrap.Params{Timeout: 30 * time.Second}, func(ctx context.Context) error {
		if _, err := r.db.DB().ExecContext(ctx, collectionsSQL.SchemaSQL); err != nil {
			return err
		}
		return callbackutil.BootstrapTable(ctx, r.db.DB(), "collections_clb_event")
	})
}
func (r *Repository) transaction(ctx context.Context, timeout time.Duration, readOnly bool, fn func(context.Context, *collectionsSQL.Queries, *sql.Tx) error) error {
	_, err := sqlwrap.Transaction(ctx, r.db, sqlwrap.Params{Timeout: timeout}, func(ctx context.Context, tx *sql.Tx) (struct{}, error) {
		if readOnly {
			if _, err := tx.ExecContext(ctx, "SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY"); err != nil {
				return struct{}{}, err
			}
		}
		return struct{}{}, fn(ctx, collectionsSQL.New(tx), tx)
	})
	return err
}
func validKey(s string) bool {
	return len(s) > 0 && len(s) <= 191 && s == strings.TrimSpace(s) && utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}
func validateIdentity(i services.Identity) error {
	if err := i.Validate(); err != nil {
		return err
	}
	if !validKey(i.PlatformUserID) {
		return ErrInvalid
	}
	return nil
}
func Subject(p model.ReadParams) (services.Identity, error) {
	if err := validateIdentity(p.Identity); err != nil {
		return services.Identity{}, err
	}
	if p.Subject == nil {
		return p.Identity, nil
	}
	if err := validateIdentity(*p.Subject); err != nil {
		return services.Identity{}, err
	}
	if p.Subject.WorkspaceID != p.Identity.WorkspaceID || p.Subject.AppID != p.Identity.AppID {
		return services.Identity{}, ErrForbidden
	}
	return *p.Subject, nil
}
func page(p model.Page) (int32, int32) {
	if p.Limit <= 0 {
		p.Limit = 100
	}
	if p.Limit > 1000 {
		p.Limit = 1000
	}
	if p.Offset < 0 {
		p.Offset = 0
	}
	return p.Limit, p.Offset
}

// DomainError maps expected storage absence without hiding other database failures.
func DomainError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
func (r *Repository) invalidate(w string) {
	err := r.db.BumpCacheVersion("collections", "catalog", w)
	if err != nil && r.options.OnCacheInvalidationError != nil {
		func() { defer func() { _ = recover() }(); r.options.OnCacheInvalidationError(err) }()
	}
}
func (r *Repository) ReconcileArchiveImport(ctx context.Context, w string) error {
	if err := services.ValidateWorkspaceID(w); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	r.invalidate(w)
	return nil
}
