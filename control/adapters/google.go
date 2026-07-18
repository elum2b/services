package auth

import (
	"context"

	"github.com/elum2b/services/control/service/admin"
)

const ProviderGoogle = "google"

func NewGoogle(config OAuth2ProviderConfig) (*OAuth2, error) {
	config.TokenURL = firstNonEmpty(config.TokenURL, "https://oauth2.googleapis.com/token")
	config.UserInfoURL = firstNonEmpty(config.UserInfoURL, "https://openidconnect.googleapis.com/v1/userinfo")
	if len(config.Scopes) == 0 {
		config.Scopes = []string{"openid", "profile", "email"}
	}
	if len(config.Mapping.Subject) == 0 {
		config.Mapping.Subject = []string{"sub"}
	}
	if len(config.Mapping.DisplayName) == 0 {
		config.Mapping.DisplayName = []string{"name", "email"}
	}
	return newOAuth2Provider(ProviderGoogle, config)
}

func Google(ctx context.Context, params OAuth2AuthParams) (admin.AuthIdentityParams, error) {
	provider, err := NewGoogle(oAuth2ProviderConfigFromAuthParams(params))
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
