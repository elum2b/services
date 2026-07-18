package auth

import (
	"context"

	"github.com/elum2b/services/control/service/admin"
)

const ProviderYandex = "yandex"

func NewYandex(config OAuth2ProviderConfig) (*OAuth2, error) {
	config.TokenURL = firstNonEmpty(config.TokenURL, "https://oauth.yandex.ru/token")
	config.UserInfoURL = firstNonEmpty(config.UserInfoURL, "https://login.yandex.ru/info?format=json")
	if len(config.Mapping.Subject) == 0 {
		config.Mapping.Subject = []string{"id", "client_id"}
	}
	if len(config.Mapping.DisplayName) == 0 {
		config.Mapping.DisplayName = []string{"real_name", "display_name", "login", "default_email"}
	}
	return newOAuth2Provider(ProviderYandex, config)
}

func Yandex(ctx context.Context, params OAuth2AuthParams) (admin.AuthIdentityParams, error) {
	provider, err := NewYandex(oAuth2ProviderConfigFromAuthParams(params))
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
