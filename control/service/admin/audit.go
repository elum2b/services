package admin

import (
	"context"

	services "github.com/elum2b/services"
)

func (a *Admin) ListAudit(ctx context.Context, workspaceID string, page Page) ([]AuditEventModel, error) {
	if err := services.ValidateWorkspaceID(workspaceID); err != nil {
		return nil, err
	}

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	limit, offset := normalizePage(page)
	items, err := a.repository.ListAudit(mergedCtx, workspaceID, limit, offset)
	if err != nil {
		return nil, err
	}
	result := make([]AuditEventModel, 0, len(items))
	for _, item := range items {
		result = append(
			result,
			AuditEventModel{
				ID:          item.ID,
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
			},
		)
	}
	return result, nil
}
