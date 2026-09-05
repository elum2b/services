package repository

import (
	"context"
	"errors"

	sqlwrap "github.com/elum2b/services/internal/utils/sql"
)

// RefreshArchiveCache recovers a commit interrupted before cache invalidation,
// even when the uploaded archive is no longer available.
func (r *Repository) RefreshArchiveCache(ctx context.Context, workspaceID string) error {
	if err := requireWorkspace(workspaceID); err != nil {
		return err
	}
	ids, err := r.q.AdminListOfferIDs(ctx, workspaceID)
	if err != nil {
		return err
	}
	r.invalidateCPACache(workspaceID, ids...)
	return nil
}

const (
	cpaCacheScopeOffer     = "offer"
	cpaCacheScopeAdminList = "admin_list"
	cpaCacheScopeUserList  = "user_list"
)

func cpaCacheKey(parts ...any) string {
	args := append([]any{"cpa"}, parts...)
	return sqlwrap.CreateKey(args...)
}

func cpaCacheVersionScope(workspaceID, scope string, values ...string) []any {
	parts := make([]any, 0, 4+len(values))

	parts = append(parts, "cpa", "cache", workspaceID, scope)

	for _, value := range values {
		parts = append(parts, value)
	}

	return parts
}

func cpaOfferCacheVersionScope(workspaceID, cpaID string) []any {
	return cpaCacheVersionScope(workspaceID, cpaCacheScopeOffer, cpaID)
}

func cpaAdminListCacheVersionScope(workspaceID string) []any {
	return cpaCacheVersionScope(workspaceID, cpaCacheScopeAdminList)
}

func cpaUserListCacheVersionScope(workspaceID string) []any {
	return cpaCacheVersionScope(workspaceID, cpaCacheScopeUserList)
}

func (r *Repository) invalidateCPACache(workspaceID string, cpaIDs ...string) {
	if r == nil || r.db == nil || workspaceID == "" {
		return
	}

	err := errors.Join(
		r.db.BumpCacheVersion(cpaAdminListCacheVersionScope(workspaceID)...),
		r.db.BumpCacheVersion(cpaUserListCacheVersionScope(workspaceID)...),
	)
	seen := make(map[string]struct{}, len(cpaIDs))

	for _, cpaID := range cpaIDs {
		if cpaID == "" {
			continue
		}

		if _, exists := seen[cpaID]; exists {
			continue
		}

		seen[cpaID] = struct{}{}
		err = errors.Join(
			err,
			r.db.BumpCacheVersion(
				cpaOfferCacheVersionScope(workspaceID, cpaID)...),
		)
	}

	r.reportCacheInvalidationError(err)
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
