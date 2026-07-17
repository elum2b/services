package repository

import (
	"context"
	"errors"

	services "github.com/elum2b/services"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	paymentsqlc "github.com/elum2b/services/payment/sqlc"
)

const paymentGlobalCacheScope = "*"

func paymentCacheKey(parts ...any) string {
	args := append([]any{"payment"}, parts...)
	return sqlwrap.CreateKey(args...)
}

func paymentCacheVersionScope(scope string) []any {
	return []any{"payment", "cache", scope}
}

func queryPaymentCache[T any](
	ctx context.Context,
	repository *PaymentRepository,
	scope string,
	key string,
	loader func(context.Context) (T, error),
) (T, error) {
	globalVersion := repository.db.CacheVersion(paymentCacheVersionScope(paymentGlobalCacheScope)...)
	value, err := sqlwrap.Query(ctx, repository.db, sqlwrap.Params{
		Key:               sqlwrap.CreateKey(key, globalVersion),
		Timeout:           repository.timeout,
		CacheL1Delay:      repository.cacheL1,
		CacheL2Delay:      repository.cacheL2,
		CacheVersionScope: paymentCacheVersionScope(scope),
	}, loader)
	return value, err
}

func queryPaymentVersionedCache[T any](
	ctx context.Context,
	repository *PaymentRepository,
	scope string,
	versionScope []any,
	key string,
	loader func(context.Context) (T, error),
) (T, error) {
	_ = scope
	return sqlwrap.Query(ctx, repository.db, sqlwrap.Params{
		Key:               key,
		Timeout:           repository.timeout,
		CacheL1Delay:      repository.cacheL1,
		CacheL2Delay:      repository.cacheL2,
		CacheVersionScope: versionScope,
	}, loader)
}

func paymentProductLimitConfigVersionScope(workspaceID string) []any {
	return []any{"payment", "product_limit_config", workspaceID}
}

func cloneSlice[T any](items []T) []T {
	if len(items) == 0 {
		return nil
	}
	out := make([]T, len(items))
	copy(out, items)
	return out
}

func InvalidateWorkspaceCache(db *sqlwrap.Client, workspaceID string) error {
	if err := services.ValidateWorkspaceID(workspaceID); err != nil {
		return err
	}
	if db == nil {
		return nil
	}

	return errors.Join(
		db.BumpCacheVersion(paymentCacheVersionScope(workspaceID)...),
		db.BumpCacheVersion(paymentProductLimitConfigVersionScope(workspaceID)...),
	)
}

func InvalidateAllCache(db *sqlwrap.Client) error {
	if db == nil {
		return nil
	}
	return db.BumpCacheVersion(paymentCacheVersionScope(paymentGlobalCacheScope)...)
}

func (r *PaymentRepository) invalidateWorkspaceCache(workspaceID string) error {
	if r == nil {
		return nil
	}
	if r.inTx {
		if r.pendingWorkspaceInvalidations == nil {
			r.pendingWorkspaceInvalidations = make(map[string]struct{})
		}
		r.pendingWorkspaceInvalidations[workspaceID] = struct{}{}
		return nil
	}
	err := InvalidateWorkspaceCache(r.db, workspaceID)
	r.reportCacheInvalidationError(err)
	return nil
}

func (r *PaymentRepository) invalidateAllCache() error {
	if r == nil {
		return nil
	}
	if r.inTx {
		r.pendingInvalidateAll = true
		return nil
	}
	err := InvalidateAllCache(r.db)
	r.reportCacheInvalidationError(err)
	return nil
}

func (r *PaymentRepository) reportCacheInvalidationError(err error) {
	if err == nil || r == nil || r.onCacheInvalidationError == nil {
		return
	}
	defer func() {
		_ = recover()
	}()
	r.onCacheInvalidationError(err)
}

func (r *PaymentRepository) RebuildWorkspaceProductCache(ctx context.Context, workspaceID string) error {
	workspaceID, err := requireWorkspaceID(workspaceID)
	if err != nil {
		return err
	}
	err = r.inTransaction(ctx, func(tx *PaymentRepository) error {
		if _, err := tx.q.DeleteWorkspaceProductCache(ctx, workspaceID); err != nil {
			return err
		}
		return tx.q.RebuildWorkspaceProductCache(ctx, paymentsqlc.RebuildWorkspaceProductCacheParams{
			WorkspaceID:   workspaceID,
			WorkspaceID_2: workspaceID,
		})
	})
	if err != nil {
		return err
	}
	return r.invalidateWorkspaceCache(workspaceID)
}

func (r *PaymentRepository) RebuildProductCache(ctx context.Context, workspaceID string, productID string) error {
	workspaceID, err := requireWorkspaceID(workspaceID)
	if err != nil {
		return err
	}
	err = r.inTransaction(ctx, func(tx *PaymentRepository) error {
		if _, err := tx.q.DeleteProductCache(ctx, paymentsqlc.DeleteProductCacheParams{
			WorkspaceID: workspaceID,
			ProductID:   productID,
		}); err != nil {
			return err
		}
		return tx.q.RebuildProductCache(ctx, paymentsqlc.RebuildProductCacheParams{
			WorkspaceID:   workspaceID,
			WorkspaceID_2: workspaceID,
			ID:            productID,
		})
	})
	if err != nil {
		return err
	}
	return r.invalidateWorkspaceCache(workspaceID)
}
