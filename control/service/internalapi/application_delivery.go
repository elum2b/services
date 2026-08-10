package internalapi

import "context"

type ApplicationDeliveryRequest struct {
	WorkspaceID string
	AppID       int64
	PlatformID  int64
}
type ApplicationDeliveryEndpoint struct {
	URL       string
	Secret    string
	IsEnabled bool
}

func (i *Internal) GetApplicationDeliveryEndpoint(
	ctx context.Context,
	params ApplicationDeliveryRequest,
) (ApplicationDeliveryEndpoint, error) {
	mergedCtx, cancel := i.withContext(ctx)
	defer cancel()

	value, err := i.repository.GetApplicationDeliveryEndpoint(
		mergedCtx,
		params.WorkspaceID,
		params.AppID,
		params.PlatformID,
	)
	if err != nil {
		return ApplicationDeliveryEndpoint{}, err
	}

	return ApplicationDeliveryEndpoint{
		URL:       value.URL,
		Secret:    value.Secret,
		IsEnabled: value.IsEnabled,
	}, nil
}
