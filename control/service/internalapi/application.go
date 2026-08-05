package internalapi

import (
	"context"
	"strings"
	"time"

	services "github.com/elum2b/services"
	"github.com/elum2b/services/control/repository"
	launchsign "github.com/elum2b/services/internal/utils/sign"
)

type AuthenticateApplicationUserRequest struct {
	WorkspaceID string
	AppID       int64
	PlatformID  int64
	Launch      string
}

func (i *Internal) AuthenticateApplicationUser(
	ctx context.Context,
	params AuthenticateApplicationUserRequest,
) (services.Identity, error) {

	mergedCtx, cancel := i.withContext(ctx)
	defer cancel()

	workspaceID := strings.TrimSpace(params.WorkspaceID)
	configuration, err := i.repository.GetApplicationAuthentication(
		mergedCtx,
		workspaceID,
		params.AppID,
		params.PlatformID,
	)
	if err != nil {
		return services.Identity{}, err
	}
	if !configuration.IsEnabled {
		return services.Identity{}, repository.ErrForbidden
	}

	launch, err := launchsign.Verify(
		launchsign.Provider(configuration.Provider),
		params.Launch,
		configuration.Secret,
		params.AppID,
	)
	if err != nil {
		return services.Identity{}, repository.ErrForbidden
	}

	now := time.Now().UTC()
	if launch.IssuedAt.After(now) || now.Sub(launch.IssuedAt) > time.Duration(
		configuration.MaxAuthenticationAgeSeconds,
	)*time.Second {
		return services.Identity{}, repository.ErrForbidden
	}

	identity := services.Identity{
		WorkspaceID:    workspaceID,
		AppID:          params.AppID,
		PlatformID:     params.PlatformID,
		Platform:       string(configuration.Provider),
		PlatformUserID: launch.PlatformUserID,
	}
	if err := identity.Validate(); err != nil {
		return services.Identity{}, err
	}
	return identity, nil

}
