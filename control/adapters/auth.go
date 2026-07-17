package auth

import (
	"context"
	"strings"

	"github.com/elum2b/services/control/service/admin"
	serviceerrors "github.com/elum2b/services/errors"
)

type Auth struct {
	admin     Admin
	providers map[string]Provider
}

func New(admin Admin, providers ...Provider) (*Auth, error) {
	if admin == nil {
		return nil, ErrAdminRequired
	}
	a := &Auth{admin: admin, providers: make(map[string]Provider, len(providers))}
	for _, provider := range providers {
		if err := a.Register(provider); err != nil {
			return nil, err
		}
	}
	return a, nil
}

func (a *Auth) Register(provider Provider) error {
	if provider == nil {
		return ErrProviderRequired
	}
	key := normalizeProvider(provider.Provider())
	if key == "" {
		return ErrProviderRequired
	}
	if a.providers == nil {
		a.providers = make(map[string]Provider)
	}
	if _, exists := a.providers[key]; exists {
		return ErrProviderExists
	}
	a.providers[key] = provider
	return nil
}

func (a *Auth) Authenticate(ctx context.Context, request Request) (admin.AuthResult, error) {
	if a == nil || a.admin == nil {
		return admin.AuthResult{}, ErrAdminRequired
	}
	providerKey := normalizeProvider(request.Provider)
	if providerKey == "" {
		return admin.AuthResult{}, ErrProviderRequired
	}
	provider, ok := a.providers[providerKey]
	if !ok {
		return admin.AuthResult{}, ErrProviderNotFound
	}
	identity, err := provider.Resolve(ctx, request)
	if err != nil {
		return admin.AuthResult{}, err
	}
	identity.Provider = firstNonEmpty(identity.Provider, provider.Provider(), providerKey)
	identity.Provider = normalizeProvider(identity.Provider)
	identity.Subject = strings.TrimSpace(identity.Subject)
	if identity.Subject == "" {
		return admin.AuthResult{}, ErrSubjectRequired
	}
	result, err := a.admin.CompleteAuth(ctx, admin.AuthIdentityParams{
		Provider:    identity.Provider,
		Subject:     identity.Subject,
		DisplayName: strings.TrimSpace(identity.DisplayName),
		Payload:     identity.Payload,
		IP:          strings.TrimSpace(request.IP),
		UserAgent:   strings.TrimSpace(request.UserAgent),
		BindToIP:    request.BindToIP,
		ExpiresAt:   request.ExpiresAt,
	})
	if err != nil {
		return admin.AuthResult{}, serviceerrors.Normalize(err, serviceerrors.CodeInternalError, "control auth failed")
	}
	return result, nil
}

func normalizeProvider(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
