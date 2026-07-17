package repository

import (
	"errors"

	sqlwrap "github.com/elum2b/services/internal/utils/sql"
)

const (
	promoCacheAdminPromo         = "admin_promo"
	promoCacheAdminList          = "admin_list"
	promoCacheAdminLocalization  = "admin_localization"
	promoCacheAdminLocalizations = "admin_localizations"
	promoCacheAdminReward        = "admin_reward"
	promoCacheAdminRewards       = "admin_rewards"
)

func promoCacheKey(method, workspaceID string, parts ...any) string {
	args := append([]any{"promo", method, workspaceID}, parts...)
	return sqlwrap.CreateKey(args...)
}

func promoCacheScope(method, workspaceID string) []any {
	return []any{"promo", "cache", method, workspaceID}
}

func (r *Repository) invalidatePromoCache(workspaceID string) error {
	if r == nil || r.db == nil || workspaceID == "" {
		return nil
	}
	err := errors.Join(
		r.db.BumpCacheVersion(promoCacheScope(promoCacheAdminPromo, workspaceID)...),
		r.db.BumpCacheVersion(promoCacheScope(promoCacheAdminList, workspaceID)...),
		r.db.BumpCacheVersion(promoCacheScope(promoCacheAdminLocalization, workspaceID)...),
		r.db.BumpCacheVersion(promoCacheScope(promoCacheAdminLocalizations, workspaceID)...),
		r.db.BumpCacheVersion(promoCacheScope(promoCacheAdminReward, workspaceID)...),
		r.db.BumpCacheVersion(promoCacheScope(promoCacheAdminRewards, workspaceID)...),
	)
	r.reportCacheInvalidationError(err)
	return nil
}

func (r *Repository) reportCacheInvalidationError(err error) {
	if err == nil || r == nil || r.onCacheInvalidationError == nil {
		return
	}
	defer func() {
		_ = recover()
	}()
	r.onCacheInvalidationError(err)
}
