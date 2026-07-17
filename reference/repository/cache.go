package repository

import (
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
)

const (
	referenceCacheGet                    = "get"
	referenceCacheResolve                = "resolve"
	referenceCacheList                   = "list"
	referenceCacheAdminGet               = "admin_get"
	referenceCacheAdminList              = "admin_list"
	referenceCacheAdminGetLocalization   = "admin_get_localization"
	referenceCacheAdminListLocalizations = "admin_list_localizations"
	referenceCacheAdminStats             = "admin_stats"
)

var (
	referenceItemMutationCacheMethods = []string{
		referenceCacheGet,
		referenceCacheResolve,
		referenceCacheList,
		referenceCacheAdminGet,
		referenceCacheAdminList,
		referenceCacheAdminStats,
	}
	referenceLocalizationMutationCacheMethods = []string{
		referenceCacheGet,
		referenceCacheResolve,
		referenceCacheList,
		referenceCacheAdminGet,
		referenceCacheAdminGetLocalization,
		referenceCacheAdminListLocalizations,
	}
)

func (r *Repository) referenceCacheKey(method, workspaceID string, parts ...any) string {
	args := append([]any{"reference", method, workspaceID}, parts...)
	return sqlwrap.CreateKey(args...)
}

func referenceCacheScope(method, workspaceID string) []any {
	return []any{"reference", method, workspaceID}
}

func (r *Repository) bumpReferenceCacheVersions(workspaceID string, methods ...string) error {
	if r == nil || r.db == nil || workspaceID == "" {
		return nil
	}
	var result error
	for _, method := range methods {
		if method == "" {
			continue
		}
		if err := r.db.BumpCacheVersion(referenceCacheScope(method, workspaceID)...); err != nil && result == nil {
			result = err
		}
	}
	r.reportCacheInvalidationError(result)
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
