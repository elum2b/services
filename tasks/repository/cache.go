package repository

import (
	"context"

	sqlwrap "github.com/elum2b/services/internal/utils/sql"
)

func activeCatalogCacheKey(workspaceID, locale, groupKey string) string {
	return sqlwrap.CreateKey("tasks", "active_catalog", workspaceID, locale, groupKey)
}

func activeComplexConditionsCacheKey(workspaceID string) string {
	return sqlwrap.CreateKey("tasks", "active_complex_conditions", workspaceID)
}

func recordCatalogCacheKey(workspaceID, actionKey string) string {
	return sqlwrap.CreateKey("tasks", "record_catalog", workspaceID, actionKey)
}

func claimCatalogByIDCacheKey(workspaceID string, id uint64) string {
	return sqlwrap.CreateKey("tasks", "claim_catalog_id", workspaceID, id)
}

func claimCatalogByKeyCacheKey(workspaceID, key string) string {
	return sqlwrap.CreateKey("tasks", "claim_catalog_key", workspaceID, key)
}

func integrationCheckTaskByIDCacheKey(workspaceID string, id uint64) string {
	return sqlwrap.CreateKey("tasks", "integration_check_task_id", workspaceID, id)
}

func integrationCheckTaskByKeyCacheKey(workspaceID, key string) string {
	return sqlwrap.CreateKey("tasks", "integration_check_task_key", workspaceID, key)
}

func rewardsCatalogCacheKey(workspaceID string, taskID uint64) string {
	return sqlwrap.CreateKey("tasks", "rewards_catalog", workspaceID, taskID)
}

func nextSequenceTaskCacheKey(workspaceID, sequenceKey string, sequencePosition uint32) string {
	return sqlwrap.CreateKey("tasks", "next_sequence_task", workspaceID, sequenceKey, sequencePosition)
}

func adminGetTaskCacheKey(workspaceID string, id uint64) string {
	return sqlwrap.CreateKey("tasks", "admin_get_task", workspaceID, id)
}

func adminListTasksCacheKey(workspaceID, groupKey string, limit, offset int32) string {
	return sqlwrap.CreateKey("tasks", "admin_list_tasks", workspaceID, groupKey, limit, offset)
}

func partnerConfigCacheScope(workspaceID string) []any {
	return []any{"tasks", "partner_config", workspaceID}
}

func partnerConfigCacheKey(workspaceID, provider, groupKey, platform string) string {
	return sqlwrap.CreateKey("tasks", "partner_config", workspaceID, provider, groupKey, platform)
}

func partnerConfigWebhookCacheKey(workspaceID, secret string) string {
	return sqlwrap.CreateKey("tasks", "partner_config_webhook", workspaceID, secret)
}

func partnerConfigListCacheKey(workspaceID string) string {
	return sqlwrap.CreateKey("tasks", "partner_config_list", workspaceID)
}

func partnerScriptCacheScope() []any {
	return []any{"tasks", "partner_script"}
}

func partnerScriptCacheKey(provider string) string {
	return sqlwrap.CreateKey("tasks", "partner_script", provider)
}

func partnerScriptListCacheKey() string {
	return sqlwrap.CreateKey("tasks", "partner_script_list")
}

func taskCatalogCacheScope(workspaceID string) []any {
	return []any{"tasks", "catalog", workspaceID}
}

func (r *Repository) invalidateTaskCache(_ context.Context, workspaceID string) error {
	if r == nil || r.db == nil || workspaceID == "" {
		return nil
	}
	if err := r.db.BumpCacheVersion(taskCatalogCacheScope(workspaceID)...); err != nil {
		r.reportCacheInvalidationError(err)
	}
	return nil
}

func (r *Repository) bumpPartnerConfigCache(workspaceID string) error {
	if r == nil || r.db == nil || workspaceID == "" {
		return nil
	}
	if err := r.db.BumpCacheVersion(partnerConfigCacheScope(workspaceID)...); err != nil {
		r.reportCacheInvalidationError(err)
	}
	return nil
}

func (r *Repository) bumpPartnerScriptCache() error {
	if r == nil || r.db == nil {
		return nil
	}
	if err := r.db.BumpCacheVersion(partnerScriptCacheScope()...); err != nil {
		r.reportCacheInvalidationError(err)
	}
	return nil
}

func (r *Repository) reportCacheInvalidationError(err error) {
	if err != nil && r != nil && r.onCacheInvalidationError != nil {
		r.onCacheInvalidationError(err)
	}
}
