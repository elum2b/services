package auth

import (
	"context"

	"github.com/elum2b/services/control/service/admin"
)

const ProviderGitLab = "gitlab"

func NewGitLab(config OAuth2ProviderConfig) (*OAuth2, error) {
	config.TokenURL = firstNonEmpty(config.TokenURL, "https://gitlab.com/oauth/token")
	config.UserInfoURL = firstNonEmpty(config.UserInfoURL, "https://gitlab.com/api/v4/user")
	if len(config.Scopes) == 0 {
		config.Scopes = []string{"read_user"}
	}
	if len(config.Mapping.Subject) == 0 {
		config.Mapping.Subject = []string{"id"}
	}
	if len(config.Mapping.DisplayName) == 0 {
		config.Mapping.DisplayName = []string{"name", "username"}
	}
	return newOAuth2Provider(ProviderGitLab, config)
}

func GitLab(ctx context.Context, params OAuth2AuthParams) (admin.AuthIdentityParams, error) {
	provider, err := NewGitLab(oAuth2ProviderConfigFromAuthParams(params))
	if err != nil {
		return admin.AuthIdentityParams{}, err
	}
	identity, err := provider.Resolve(ctx, Request{Code: params.Code, AccessToken: params.AccessToken, RedirectURI: params.RedirectURI})
	if err != nil {
		return admin.AuthIdentityParams{}, err
	}
	return identityAuthParams(identity, params.IP, params.UserAgent, params.BindToIP, params.ExpiresAt), nil
}
