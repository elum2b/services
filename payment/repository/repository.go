package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	services "github.com/elum2b/services"
	serviceerrors "github.com/elum2b/services/errors"
	callbackutil "github.com/elum2b/services/internal/utils/callback"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	"github.com/jackc/pgx/v5/pgconn"

	paymentsqlc "github.com/elum2b/services/payment/sqlc"
)

type PaymentRepository struct {
	db                            *sqlwrap.Client
	q                             *paymentsqlc.Queries
	callbacks                     *callbackutil.Store
	executor                      paymentsqlc.DBTX
	inTx                          bool
	timeout                       time.Duration
	cacheL1                       time.Duration
	cacheL2                       time.Duration
	pendingWorkspaceInvalidations map[string]struct{}
	pendingInvalidateAll          bool
	onCacheInvalidationError      func(error)
}

type Options struct {
	QueryTimeout             time.Duration
	CacheL1Delay             time.Duration
	CacheL2Delay             time.Duration
	OnCacheInvalidationError func(error)
}

const bootstrapQueryTimeout = 30 * time.Second

var (
	ErrWorkspaceRequired    = serviceerrors.New(serviceerrors.CodeInvalidFields, "payment workspace id is required")
	ErrInvalidProvider      = serviceerrors.New(serviceerrors.CodeInvalidFields, "payment provider is invalid")
	ErrInvalidAsset         = serviceerrors.New(serviceerrors.CodeInvalidFields, "payment asset is invalid")
	ErrInvalidProviderAsset = serviceerrors.New(serviceerrors.CodeInvalidFields, "payment provider asset is invalid")
	ErrInvalidDateRange     = serviceerrors.New(serviceerrors.CodeInvalidFields, "payment date range is invalid")
)

func NewPaymentRepository(db *sqlwrap.Client) *PaymentRepository {
	return NewPaymentRepositoryWithOptions(db, Options{
		CacheL1Delay: 10 * time.Minute,
		CacheL2Delay: 10 * time.Minute,
	})
}

func NewPaymentRepositoryWithOptions(db *sqlwrap.Client, options Options) *PaymentRepository {
	timeout := queryTimeout(options.QueryTimeout)
	executor := db.WithQueryTimeout(timeout)
	return &PaymentRepository{
		db:                       db,
		q:                        paymentsqlc.New(executor),
		callbacks:                callbackutil.NewWithTable(db.DB(), callbackutil.PaymentTable),
		executor:                 executor,
		timeout:                  timeout,
		cacheL1:                  options.CacheL1Delay,
		cacheL2:                  options.CacheL2Delay,
		onCacheInvalidationError: options.OnCacheInvalidationError,
	}
}

func NewPreparedPaymentRepository(ctx context.Context, db *sqlwrap.Client) (*PaymentRepository, error) {
	return NewPreparedPaymentRepositoryWithOptions(ctx, db, Options{})
}

func NewPreparedPaymentRepositoryWithOptions(
	_ context.Context,
	db *sqlwrap.Client,
	options Options,
) (*PaymentRepository, error) {
	return NewPaymentRepositoryWithOptions(db, options), nil
}

func (r *PaymentRepository) Close() error {
	if r == nil || r.q == nil {
		return nil
	}
	var callbackErr error
	if r.callbacks != nil {
		callbackErr = r.callbacks.Close()
	}
	return errors.Join(r.q.Close(), callbackErr)
}

func (r *PaymentRepository) WithTx(ctx context.Context, fn func(*PaymentRepository) error) error {
	pendingWorkspaces := make(map[string]struct{})
	pendingInvalidateAll := false
	_, err := sqlwrap.Transaction(
		ctx,
		r.db,
		sqlwrap.Params{Timeout: r.timeout},
		func(ctx context.Context, tx *sql.Tx) (struct{}, error) {
			txRepo := &PaymentRepository{
				db:                            r.db,
				q:                             r.q.WithTx(tx),
				callbacks:                     r.callbacks.WithTx(tx),
				executor:                      tx,
				inTx:                          true,
				timeout:                       r.timeout,
				cacheL1:                       r.cacheL1,
				cacheL2:                       r.cacheL2,
				pendingWorkspaceInvalidations: pendingWorkspaces,
				onCacheInvalidationError:      r.onCacheInvalidationError,
			}
			callbackErr := fn(txRepo)
			pendingInvalidateAll = txRepo.pendingInvalidateAll
			return struct{}{}, callbackErr
		},
	)
	if err != nil {
		return err
	}

	var cacheErr error
	if pendingInvalidateAll {
		cacheErr = InvalidateAllCache(r.db)
	} else {
		for workspaceID := range pendingWorkspaces {
			cacheErr = errors.Join(cacheErr, InvalidateWorkspaceCache(r.db, workspaceID))
		}
	}
	r.reportCacheInvalidationError(cacheErr)
	return nil
}

func (r *PaymentRepository) inTransaction(ctx context.Context, fn func(*PaymentRepository) error) error {
	if r.inTx {
		return fn(r)
	}
	return r.WithTx(ctx, fn)
}

func (r *PaymentRepository) Bootstrap(ctx context.Context, schemaPath ...string) error {
	raw := paymentsqlc.SchemaSQL
	if len(schemaPath) > 0 && strings.TrimSpace(schemaPath[0]) != "" {
		data, err := os.ReadFile(schemaPath[0])
		if err != nil {
			return err
		}
		raw = string(data)
	}

	statements, err := sqlwrap.SplitStatements(raw)
	if err != nil {
		return fmt.Errorf("payment schema SQL parse failed: %w", err)
	}
	for _, stmt := range statements {
		if err := sqlwrap.Exec(ctx, r.db, sqlwrap.Params{Timeout: bootstrapQueryTimeout}, func(ctx context.Context) error {
			_, err := r.db.DB().ExecContext(ctx, stmt)
			return err
		}); err != nil {
			if isDuplicateTypeStatement(stmt, err) {
				continue
			}
			return fmt.Errorf("statement failed: %w\n%s", err, stmt)
		}
	}
	if err := r.applySchemaUpgrades(ctx); err != nil {
		return err
	}

	if err := sqlwrap.Exec(ctx, r.db, sqlwrap.Params{Timeout: bootstrapQueryTimeout}, func(ctx context.Context) error {
		return callbackutil.BootstrapTable(ctx, r.db.DB(), callbackutil.PaymentTable)
	}); err != nil {
		return err
	}
	if err := r.applySQL(ctx, paymentsqlc.TriggerSQL, "trigger"); err != nil {
		return err
	}
	return r.applySQL(ctx, paymentsqlc.EventSQL, "event")
}

func isDuplicateTypeStatement(stmt string, err error) bool {
	if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(stmt)), "CREATE TYPE ") {
		return false
	}
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "42710"
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError

	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func (r *PaymentRepository) applySchemaUpgrades(ctx context.Context) error {
	_ = ctx
	return nil
}

func (r *PaymentRepository) applySQL(ctx context.Context, raw, source string) error {
	statements, err := sqlwrap.SplitStatements(raw)
	if err != nil {
		return fmt.Errorf("payment %s SQL parse failed: %w", source, err)
	}
	for _, statement := range statements {
		if err := sqlwrap.Exec(ctx, r.db, sqlwrap.Params{Timeout: bootstrapQueryTimeout}, func(ctx context.Context) error {
			_, err := r.db.DB().ExecContext(ctx, statement)
			return err
		}); err != nil {
			return fmt.Errorf("payment %s SQL statement failed: %w\n%s", source, err, statement)
		}
	}
	return nil
}

func queryTimeout(value time.Duration) time.Duration {
	if value <= 0 {
		return time.Second
	}
	return value
}

func (r *PaymentRepository) ListProviders(ctx context.Context) ([]AdminProviderModel, error) {
	key := paymentCacheKey("providers")
	providers, err := queryPaymentCache(
		ctx,
		r,
		paymentGlobalCacheScope,
		key,
		func(ctx context.Context) ([]paymentsqlc.PaymentProvider, error) {
			return r.q.ListProviders(ctx)
		},
	)
	if err != nil {
		return nil, err
	}
	return mapAdminSlice(cloneSlice(providers), mapAdminProvider), nil
}

func (r *PaymentRepository) ListAssets(ctx context.Context) ([]AdminAssetModel, error) {
	key := paymentCacheKey("assets")
	assets, err := queryPaymentCache(
		ctx,
		r,
		paymentGlobalCacheScope,
		key,
		func(ctx context.Context) ([]paymentsqlc.PaymentAsset, error) {
			return r.q.ListAssets(ctx)
		},
	)
	if err != nil {
		return nil, err
	}
	return mapAdminSlice(cloneSlice(assets), mapAdminAsset), nil
}

type AssetUpsertParams struct {
	Code            string
	Title           string
	AssetKind       string
	Scale           uint16
	Chain           *string
	Network         *string
	ContractAddress *string
	IsActive        bool
}

func (r *PaymentRepository) UpsertAsset(ctx context.Context, params AssetUpsertParams) error {
	if strings.TrimSpace(params.Code) == "" || strings.TrimSpace(params.Title) == "" ||
		params.Scale > 18 || !validAssetKind(params.AssetKind) {
		return ErrInvalidAsset
	}
	if err := r.q.UpsertAsset(ctx, paymentsqlc.UpsertAssetParams{
		Code:      params.Code,
		Title:     params.Title,
		AssetKind: paymentsqlc.PaymentAssetAssetKind(params.AssetKind),
		Scale:     int16(params.Scale),
		Chain: sqlwrap.NullFromPtr(params.Chain, func(v string) sql.NullString {
			return sql.NullString{String: v, Valid: true}
		}),
		Network: sqlwrap.NullFromPtr(params.Network, func(v string) sql.NullString {
			return sql.NullString{String: v, Valid: true}
		}),
		ContractAddress: sqlwrap.NullFromPtr(params.ContractAddress, func(v string) sql.NullString {
			return sql.NullString{String: v, Valid: true}
		}),
		IsActive: params.IsActive,
	}); err != nil {
		return err
	}
	return r.invalidateAllCache()
}

func (r *PaymentRepository) DeleteAsset(ctx context.Context, code string) (int64, error) {
	rows, err := r.q.DeleteAsset(ctx, code)
	if err != nil {
		return 0, err
	}
	return rows, r.invalidateAllCache()
}

func (r *PaymentRepository) GetProviderAsset(
	ctx context.Context,
	providerCode string,
	assetCode string,
) (AdminProviderAssetModel, error) {
	key := paymentCacheKey("provider_asset", providerCode, assetCode)
	row, err := queryPaymentCache(
		ctx,
		r,
		paymentGlobalCacheScope,
		key,
		func(ctx context.Context) (paymentsqlc.PaymentProviderAsset, error) {
			return r.q.GetProviderAsset(ctx, paymentsqlc.GetProviderAssetParams{
				ProviderCode: providerCode,
				AssetCode:    assetCode,
			})
		},
	)
	return mapAdminResult(row, err, mapAdminProviderAsset)
}

type ProviderAssetUpsertParams struct {
	ProviderCode    string
	AssetCode       string
	MinAmountMinor  *int64
	MaxAmountMinor  *int64
	MerchantAccount *string
	IsActive        bool
}

func (r *PaymentRepository) UpsertProviderAsset(ctx context.Context, params ProviderAssetUpsertParams) error {
	if strings.TrimSpace(params.ProviderCode) == "" || strings.TrimSpace(params.AssetCode) == "" ||
		(params.MinAmountMinor != nil && *params.MinAmountMinor < 0) ||
		(params.MaxAmountMinor != nil && *params.MaxAmountMinor < 0) ||
		(params.MinAmountMinor != nil && params.MaxAmountMinor != nil && *params.MinAmountMinor > *params.MaxAmountMinor) {
		return ErrInvalidProviderAsset
	}
	if err := r.q.UpsertProviderAsset(ctx, paymentsqlc.UpsertProviderAssetParams{
		ProviderCode: params.ProviderCode,
		AssetCode:    params.AssetCode,
		MinAmountMinor: sqlwrap.NullFromPtr(params.MinAmountMinor, func(v int64) sql.NullInt64 {
			return sql.NullInt64{Int64: v, Valid: true}
		}),
		MaxAmountMinor: sqlwrap.NullFromPtr(params.MaxAmountMinor, func(v int64) sql.NullInt64 {
			return sql.NullInt64{Int64: v, Valid: true}
		}),
		MerchantAccount: sqlwrap.NullFromPtr(params.MerchantAccount, func(v string) sql.NullString {
			return sql.NullString{String: v, Valid: true}
		}),
		IsActive: params.IsActive,
	}); err != nil {
		return err
	}
	return r.invalidateAllCache()
}

func (r *PaymentRepository) DeleteProviderAsset(
	ctx context.Context,
	providerCode string,
	assetCode string,
) (int64, error) {
	rows, err := r.q.DeleteProviderAsset(ctx, paymentsqlc.DeleteProviderAssetParams{
		ProviderCode: providerCode,
		AssetCode:    assetCode,
	})
	if err != nil {
		return 0, err
	}
	return rows, r.invalidateAllCache()
}

func requireWorkspaceID(workspaceID string) (string, error) {
	if err := services.ValidateWorkspaceID(workspaceID); err != nil {
		return "", err
	}

	return workspaceID, nil
}

func validAssetKind(value string) bool {
	switch value {
	case string(paymentsqlc.PaymentAssetAssetKindFiat),
		string(paymentsqlc.PaymentAssetAssetKindPlatformCurrency),
		string(paymentsqlc.PaymentAssetAssetKindCryptoNative),
		string(paymentsqlc.PaymentAssetAssetKindCryptoJetton):
		return true
	default:
		return false
	}
}
