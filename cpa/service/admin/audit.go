package admin

import (
	"context"

	"github.com/elum2b/services/cpa/model"
	"github.com/elum2b/services/cpa/repository"
	"github.com/elum2b/services/cpa/service/user"
)

type AssignmentListParams struct {
	WorkspaceID string
	CPAID       string
	Status      model.AssignmentStatus
	Page        Page
}

type CodeListParams struct {
	WorkspaceID string
	CPAID       string
	Status      model.CodeStatus
	Page        Page
}

type AssignmentEventListParams struct {
	WorkspaceID string
	CPAID       string
	EventType   model.AssignmentEventType
	Page        Page
}

func (a *Admin) GetUserAssignment(ctx context.Context, params user.GetStatusParams) (*AssignmentModel, error) {

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	value, err := a.repository.FindAssignment(mergedCtx, repository.UserScope{
		WorkspaceID:    params.Identity.WorkspaceID,
		CPAID:          params.CPAID,
		AppID:          params.Identity.AppID,
		PlatformID:     params.Identity.PlatformID,
		PlatformUserID: params.Identity.PlatformUserID,
	})
	if err != nil || value == nil {
		return nil, err
	}

	result := mapAssignment(*value)
	return &result, nil

}

func (a *Admin) ListAssignments(ctx context.Context, params AssignmentListParams) ([]AssignmentModel, error) {

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	limit, offset := normalizePage(params.Page)
	values, err := a.repository.ListAssignments(
		mergedCtx,
		params.WorkspaceID,
		params.CPAID,
		params.Status,
		limit,
		offset,
	)
	if err != nil {
		return nil, err
	}

	result := make([]AssignmentModel, 0, len(values))
	for _, value := range values {
		result = append(result, mapAssignment(value))
	}

	return result, nil

}

func (a *Admin) ListCodes(ctx context.Context, params CodeListParams) ([]CodeModel, error) {

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	limit, offset := normalizePage(params.Page)
	values, err := a.repository.ListCodes(
		mergedCtx,
		params.WorkspaceID,
		params.CPAID,
		params.Status,
		limit,
		offset,
	)
	if err != nil {
		return nil, err
	}

	result := make([]CodeModel, 0, len(values))
	for _, value := range values {
		result = append(result, CodeModel{
			ID:        value.ID,
			Code:      value.Code,
			Source:    value.Source,
			Status:    value.Status,
			CreatedAt: value.CreatedAt,
			UpdatedAt: value.UpdatedAt,
			DeletedAt: value.DeletedAt,
		})
	}

	return result, nil

}

func (a *Admin) ListAssignmentEvents(ctx context.Context, params AssignmentEventListParams) ([]AssignmentEventModel, error) {

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	limit, offset := normalizePage(params.Page)
	values, err := a.repository.ListAssignmentEvents(
		mergedCtx,
		params.WorkspaceID,
		params.CPAID,
		params.EventType,
		limit,
		offset,
	)
	if err != nil {
		return nil, err
	}

	result := make([]AssignmentEventModel, 0, len(values))
	for _, value := range values {
		result = append(result, AssignmentEventModel{
			ID:           value.ID,
			AssignmentID: value.AssignmentID,
			EventType:    value.EventType,
			OccurredAt:   value.OccurredAt,
		})
	}

	return result, nil

}

func mapAssignment(value repository.Assignment) AssignmentModel {
	return AssignmentModel{
		ID:             value.ID,
		CPAID:          value.CPAID,
		AppID:          value.AppID,
		PlatformID:     value.PlatformID,
		PlatformUserID: value.PlatformUserID,
		Code:           value.Code,
		CodeMode:       value.CodeMode,
		Status:         value.Status,
		IssuedAt:       value.IssuedAt,
		CompletedAt:    value.CompletedAt,
	}
}
