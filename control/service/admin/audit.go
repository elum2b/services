package admin

import (
	"context"

	"github.com/elum2b/services/control/repository"
)

func (a *Admin) ListGlobalAudit(
	ctx context.Context,
	page Page,
) ([]AuditEventModel, error) {

	return a.listAudit(ctx, ScopeGlobal, "", page)

}

func (a *Admin) ListWorkspaceAudit(
	ctx context.Context,
	workspaceID string,
	page Page,
) ([]AuditEventModel, error) {

	return a.listAudit(ctx, ScopeWorkspace, workspaceID, page)

}

func (a *Admin) listAudit(
	ctx context.Context,
	scope AccessScope,
	workspaceID string,
	page Page,
) ([]AuditEventModel, error) {

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	items, err := a.repository.ListAudit(
		mergedCtx,
		repository.AccessScope(scope),
		workspaceID,
		mapCursor(page),
		page.Limit,
	)
	if err != nil {
		return nil, err
	}

	result := make([]AuditEventModel, 0, len(items))
	for _, item := range items {
		result = append(result, AuditEventModel{
			ID:          item.ID,
			Scope:       AccessScope(item.Scope),
			WorkspaceID: item.WorkspaceID,
			ActorID:     item.ActorID,
			MethodKey:   item.MethodKey,
			TargetType:  item.TargetType,
			TargetID:    item.TargetID,
			Result:      item.Result,
			RequestID:   item.RequestID,
			BeforeData:  item.BeforeData,
			AfterData:   item.AfterData,
			OccurredAt:  item.OccurredAt,
		})
	}

	return result, nil

}
