package auth

import (
	"context"

	"github.com/elum2b/services/control/service/admin"
)

const ProviderDiscord = "discord"

// NewDiscord creates an OAuth2 identity provider for Discord user accounts.
// The identify scope is sufficient to obtain the immutable Discord user ID.
func NewDiscord(config OAuth2ProviderConfig) (*OAuth2, error) {

	config.TokenURL = firstNonEmpty(
		config.TokenURL,
		"https://discord.com/api/oauth2/token",
	)
	config.UserInfoURL = firstNonEmpty(
		config.UserInfoURL,
		"https://discord.com/api/users/@me",
	)

	if len(config.Scopes) == 0 {
		config.Scopes = []string{"identify"}
	}

	if len(config.Mapping.Subject) == 0 {
		config.Mapping.Subject = []string{"id"}
	}

	if len(config.Mapping.DisplayName) == 0 {
		config.Mapping.DisplayName = []string{
			"global_name",
			"username",
		}
	}

	return newOAuth2Provider(ProviderDiscord, config)
}

func Discord(
	ctx context.Context,
	params OAuth2AuthParams,
) (admin.AuthIdentityParams, error) {

	provider, err := NewDiscord(oAuth2ProviderConfigFromAuthParams(params))
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
