package admin

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/elum2b/services/control/repository"
)

func (a *Admin) UpsertApplicationPlatform(
	ctx context.Context,
	params UpsertApplicationPlatformParams,
) (ApplicationPlatformModel, error) {
	mergedCtx, cancel := a.withMutation(ctx, repository.AuditEvent{
		Scope:       repository.ScopeWorkspace,
		WorkspaceID: strings.TrimSpace(params.WorkspaceID),
		ActorID:     strings.TrimSpace(params.ActorID),
		MethodKey:   "control.workspace.update",
		TargetType:  "application_platform",
		TargetID: applicationTargetID(
			params.AppID,
			params.PlatformID,
		),
	})
	defer cancel()

	seconds, err := authenticationAgeSeconds(params.MaxAuthenticationAge)
	if err != nil {
		return ApplicationPlatformModel{}, err
	}

	value, err := a.repository.UpsertApplicationPlatform(
		mergedCtx,
		strings.TrimSpace(params.ActorID),
		repository.ApplicationPlatformInput{
			WorkspaceID: strings.TrimSpace(params.WorkspaceID),
			AppID:       params.AppID,
			PlatformID:  params.PlatformID,
			Provider: repository.ApplicationProvider(
				params.Provider,
			),
			Secret:                      params.Secret,
			MaxAuthenticationAgeSeconds: seconds,
			IsEnabled:                   params.IsEnabled,
		},
	)
	if err != nil {
		return ApplicationPlatformModel{}, err
	}

	return mapApplicationPlatform(value), nil
}

func (a *Admin) ListApplicationPlatforms(
	ctx context.Context,
	params ListApplicationPlatformsParams,
) ([]ApplicationPlatformModel, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	values, err := a.repository.ListApplicationPlatforms(
		mergedCtx,
		strings.TrimSpace(params.ActorID),
		strings.TrimSpace(params.WorkspaceID),
	)
	if err != nil {
		return nil, err
	}

	result := make([]ApplicationPlatformModel, 0, len(values))
	for _, value := range values {
		result = append(result, mapApplicationPlatform(value))
	}

	return result, nil
}

func (a *Admin) DeleteApplicationPlatform(
	ctx context.Context,
	params DeleteApplicationPlatformParams,
) (int64, error) {
	mergedCtx, cancel := a.withMutation(ctx, repository.AuditEvent{
		Scope:       repository.ScopeWorkspace,
		WorkspaceID: strings.TrimSpace(params.WorkspaceID),
		ActorID:     strings.TrimSpace(params.ActorID),
		MethodKey:   "control.workspace.update",
		TargetType:  "application_platform",
		TargetID: applicationTargetID(
			params.AppID,
			params.PlatformID,
		),
	})
	defer cancel()

	return a.repository.DeleteApplicationPlatform(
		mergedCtx,
		strings.TrimSpace(params.ActorID),
		strings.TrimSpace(params.WorkspaceID),
		params.AppID,
		params.PlatformID,
	)
}

func authenticationAgeSeconds(value time.Duration) (int32, error) {
	if value <= 0 || value > 24*time.Hour || value%time.Second != 0 {
		return 0, repository.ErrInvalidArgument
	}

	return int32(value / time.Second), nil
}

func applicationTargetID(appID, platformID int64) string {
	return strconv.FormatInt(
		appID,
		10,
	) + ":" + strconv.FormatInt(
		platformID,
		10,
	)
}

func mapApplicationPlatform(
	value repository.ApplicationPlatform,
) ApplicationPlatformModel {
	return ApplicationPlatformModel{
		WorkspaceID: value.WorkspaceID,
		AppID:       value.AppID,
		PlatformID:  value.PlatformID,
		Provider:    ApplicationProvider(value.Provider),
		MaxAuthenticationAge: time.Duration(
			value.MaxAuthenticationAgeSeconds,
		) * time.Second,
		IsEnabled: value.IsEnabled,
		CreatedAt: value.CreatedAt,
		UpdatedAt: value.UpdatedAt,
	}
}
