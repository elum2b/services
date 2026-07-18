package auth

import (
	"context"

	"github.com/elum2b/services/control/service/admin"
)

const ProviderVKID = "vk"

func NewVKID(config OAuth2ProviderConfig) (*OAuth2, error) {
	config.TokenURL = firstNonEmpty(config.TokenURL, "https://id.vk.com/oauth2/auth")
	config.UserInfoURL = firstNonEmpty(config.UserInfoURL, "https://id.vk.com/oauth2/user_info")
	if len(config.Mapping.Subject) == 0 {
		config.Mapping.Subject = []string{"user.user_id", "user.id", "id"}
	}
	if len(config.Mapping.DisplayName) == 0 {
		config.Mapping.DisplayName = []string{"user.first_name", "first_name", "name"}
	}
	return newOAuth2Provider(ProviderVKID, config)
}

func VKID(ctx context.Context, params OAuth2AuthParams) (admin.AuthIdentityParams, error) {
	provider, err := NewVKID(oAuth2ProviderConfigFromAuthParams(params))
	if err != nil {
		return admin.AuthIdentityParams{}, err
	}
	identity, err := provider.Resolve(ctx, Request{
		Code:        params.Code,
		AccessToken: params.AccessToken,
		RedirectURI: params.RedirectURI,
	})
	if err != nil {
		return admin.AuthIdentityParams{}, err
	}
	return identityAuthParams(
		identity,
		params.InviteToken,
		params.IP,
		params.UserAgent,
		params.BindToIP,
		params.ExpiresAt,
	), nil
}
