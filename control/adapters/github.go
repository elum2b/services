package auth

import (
	"context"

	"github.com/elum2b/services/control/service/admin"
)

const ProviderGitHub = "github"

func NewGitHub(config OAuth2ProviderConfig) (*OAuth2, error) {
	config.TokenURL = firstNonEmpty(config.TokenURL, "https://github.com/login/oauth/access_token")
	config.UserInfoURL = firstNonEmpty(config.UserInfoURL, "https://api.github.com/user")
	if len(config.Scopes) == 0 {
		config.Scopes = []string{"read:user", "user:email"}
	}
	if len(config.Mapping.Subject) == 0 {
		config.Mapping.Subject = []string{"id"}
	}
	if len(config.Mapping.DisplayName) == 0 {
		config.Mapping.DisplayName = []string{"name", "login"}
	}
	return newOAuth2Provider(ProviderGitHub, config)
}

func GitHub(ctx context.Context, params OAuth2AuthParams) (admin.AuthIdentityParams, error) {
	provider, err := NewGitHub(oAuth2ProviderConfigFromAuthParams(params))
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
