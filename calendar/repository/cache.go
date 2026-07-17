package repository

import (
	"errors"

	sqlwrap "github.com/elum2b/services/internal/utils/sql"
)

const (
	calendarCacheAdminCalendar      = "admin_calendar"
	calendarCacheAdminList          = "admin_list"
	calendarCacheAdminLocalization  = "admin_localization"
	calendarCacheAdminLocalizations = "admin_localizations"
	calendarCacheAdminReward        = "admin_reward"
	calendarCacheUserCalendar       = "user_calendar"
	calendarCacheUserCatalog        = "user_catalog"
)

func calendarCacheKey(method, workspaceID string, parts ...any) string {
	args := append([]any{"calendar", method, workspaceID}, parts...)
	return sqlwrap.CreateKey(args...)
}

func calendarCacheScope(method, workspaceID string) []any {
	return []any{"calendar", "cache", method, workspaceID}
}

func (r *Repository) invalidateCalendarCache(workspaceID string) {
	if r == nil || r.db == nil || workspaceID == "" {
		return
	}
	err := errors.Join(
		r.db.BumpCacheVersion(calendarCacheScope(calendarCacheAdminCalendar, workspaceID)...),
		r.db.BumpCacheVersion(calendarCacheScope(calendarCacheAdminList, workspaceID)...),
		r.db.BumpCacheVersion(calendarCacheScope(calendarCacheAdminLocalization, workspaceID)...),
		r.db.BumpCacheVersion(calendarCacheScope(calendarCacheAdminLocalizations, workspaceID)...),
		r.db.BumpCacheVersion(calendarCacheScope(calendarCacheAdminReward, workspaceID)...),
		r.db.BumpCacheVersion(calendarCacheScope(calendarCacheUserCalendar, workspaceID)...),
		r.db.BumpCacheVersion(calendarCacheScope(calendarCacheUserCatalog, workspaceID)...),
	)
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
