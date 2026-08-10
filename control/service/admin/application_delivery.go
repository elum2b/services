package admin

import (
	"context"
	"strings"

	"github.com/elum2b/services/control/repository"
)

func (a *Admin) UpsertApplicationDelivery(
	ctx context.Context,
	params UpsertApplicationDeliveryParams,
) (ApplicationDeliveryModel, error) {
	mergedCtx, cancel := a.withMutation(ctx, repository.AuditEvent{
		Scope:       repository.ScopeWorkspace,
		WorkspaceID: params.WorkspaceID,
		ActorID:     strings.TrimSpace(params.ActorID),
		MethodKey:   "control.workspace.update",
		TargetType:  "application_delivery",
		TargetID:    applicationTargetID(params.AppID, params.PlatformID),
	})
	defer cancel()

	value, err := a.repository.UpsertApplicationDelivery(
		mergedCtx,
		params.ActorID,
		repository.ApplicationDeliveryInput{
			WorkspaceID: params.WorkspaceID,
			AppID:       params.AppID,
			PlatformID:  params.PlatformID,
			URL:         params.URL,
			Secret:      params.Secret,
			IsEnabled:   params.IsEnabled,
		},
	)

	return mapApplicationDelivery(value), err
}

func (a *Admin) ListApplicationDeliveries(
	ctx context.Context,
	params ListApplicationDeliveriesParams,
) ([]ApplicationDeliveryModel, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	values, err := a.repository.ListApplicationDeliveries(
		mergedCtx,
		params.ActorID,
		params.WorkspaceID,
	)
	if err != nil {
		return nil, err
	}

	result := make([]ApplicationDeliveryModel, 0, len(values))
	for _, value := range values {
		result = append(result, mapApplicationDelivery(value))
	}

	return result, nil
}

func (a *Admin) DeleteApplicationDelivery(
	ctx context.Context,
	params DeleteApplicationDeliveryParams,
) (int64, error) {
	mergedCtx, cancel := a.withMutation(ctx, repository.AuditEvent{
		Scope:       repository.ScopeWorkspace,
		WorkspaceID: params.WorkspaceID,
		ActorID:     strings.TrimSpace(params.ActorID),
		MethodKey:   "control.workspace.update",
		TargetType:  "application_delivery",
		TargetID:    applicationTargetID(params.AppID, params.PlatformID),
	})
	defer cancel()

	return a.repository.DeleteApplicationDelivery(
		mergedCtx,
		params.ActorID,
		params.WorkspaceID,
		params.AppID,
		params.PlatformID,
	)
}

func mapApplicationDelivery(
	value repository.ApplicationDelivery,
) ApplicationDeliveryModel {
	return ApplicationDeliveryModel{
		WorkspaceID: value.WorkspaceID,
		AppID:       value.AppID,
		PlatformID:  value.PlatformID,
		URL:         value.URL,
		IsEnabled:   value.IsEnabled,
		CreatedAt:   value.CreatedAt,
		UpdatedAt:   value.UpdatedAt,
	}
}
