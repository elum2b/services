package admin

import (
	"context"
	"strings"
	"time"

	"github.com/elum2b/services/control/repository"
)

// CompleteAuth accepts an identity already verified by an external auth adapter.
// OAuth redirects and provider-specific exchange stay in the API adapter layer.
func (a *Admin) CompleteAuth(ctx context.Context, params AuthIdentityParams) (AuthResult, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	account, created, err := a.repository.AuthenticateIdentity(mergedCtx, repository.IdentityInput{
		Provider:    strings.TrimSpace(params.Provider),
		Subject:     strings.TrimSpace(params.Subject),
		DisplayName: strings.TrimSpace(params.DisplayName),
		Payload:     params.Payload,
	})
	if err != nil {
		return AuthResult{}, err
	}
	metadata := repository.SessionInput{
		IP:        params.IP,
		UserAgent: params.UserAgent,
		BindToIP:  params.BindToIP,
		ExpiresAt: params.ExpiresAt,
	}
	requiresTwoFactor, err := a.repository.RequiresTwoFactor(mergedCtx, account.ID)
	if err != nil {
		return AuthResult{}, err
	}
	if requiresTwoFactor {
		challenge, err := a.repository.CreateTwoFactorChallenge(mergedCtx, account.ID, metadata)
		if err != nil {
			return AuthResult{}, err
		}
		return AuthResult{
			Account:            mapAccount(account),
			TwoFactorRequired:  true,
			TwoFactorChallenge: challenge,
			Created:            created,
		}, nil
	}
	session, token, err := a.repository.CreateSession(mergedCtx, account.ID, metadata)
	if err != nil {
		return AuthResult{}, err
	}
	return AuthResult{
		Account:      mapAccount(account),
		Session:      mapSession(session),
		SessionToken: token,
		Created:      created,
	}, nil
}

func (a *Admin) CompleteTwoFactor(ctx context.Context, challenge, code, ip string) (AuthResult, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	session, token, err := a.repository.CompleteTwoFactorChallenge(
		mergedCtx,
		strings.TrimSpace(challenge),
		code,
		strings.TrimSpace(ip),
		time.Now(),
	)
	if err != nil {
		return AuthResult{}, err
	}
	account, err := a.repository.GetAccount(mergedCtx, session.AccountID)
	if err != nil {
		return AuthResult{}, err
	}
	return AuthResult{Account: mapAccount(account), Session: mapSession(session), SessionToken: token}, nil
}

func (a *Admin) GetAccount(ctx context.Context, accountID string) (AccountModel, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	value, err := a.repository.GetAccount(mergedCtx, strings.TrimSpace(accountID))
	return mapAccount(value), err
}

func (a *Admin) ListIdentities(ctx context.Context, accountID string) ([]IdentityModel, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	items, err := a.repository.ListIdentities(mergedCtx, strings.TrimSpace(accountID))
	if err != nil {
		return nil, err
	}
	result := make([]IdentityModel, 0, len(items))
	for _, item := range items {
		result = append(result, IdentityModel{
			AccountID: item.AccountID,
			Provider:  item.Provider,
			Subject:   item.ProviderSubject,
			CreatedAt: item.CreatedAt,
			UpdatedAt: item.UpdatedAt,
		})
	}
	return result, nil
}

func (a *Admin) BindIdentity(ctx context.Context, accountID string, params AuthIdentityParams) error {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	return a.repository.BindIdentity(mergedCtx, strings.TrimSpace(accountID), repository.IdentityInput{
		Provider:    strings.TrimSpace(params.Provider),
		Subject:     strings.TrimSpace(params.Subject),
		DisplayName: strings.TrimSpace(params.DisplayName),
		Payload:     params.Payload,
	})
}

func (a *Admin) UnbindIdentity(ctx context.Context, accountID, provider string) (int64, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	return a.repository.UnbindIdentity(mergedCtx, strings.TrimSpace(accountID), strings.TrimSpace(provider))
}

func (a *Admin) ValidateSession(ctx context.Context, rawToken, ip string) (SessionModel, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	session, err := a.repository.ValidateSession(mergedCtx, rawToken, ip)
	return mapSession(session), err
}

func (a *Admin) ListSessions(ctx context.Context, accountID string) ([]SessionModel, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	items, err := a.repository.ListSessions(mergedCtx, strings.TrimSpace(accountID))
	if err != nil {
		return nil, err
	}
	result := make([]SessionModel, 0, len(items))
	for _, item := range items {
		result = append(result, mapSession(item))
	}
	return result, nil
}

func (a *Admin) RevokeSession(ctx context.Context, accountID, sessionID string) (int64, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	return a.repository.RevokeSession(mergedCtx, strings.TrimSpace(accountID), strings.TrimSpace(sessionID))
}

func (a *Admin) RevokeAllSessions(ctx context.Context, accountID, exceptSessionID string) (int64, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	return a.repository.RevokeAllSessions(mergedCtx, strings.TrimSpace(accountID), strings.TrimSpace(exceptSessionID))
}

func (a *Admin) BeginTwoFactor(ctx context.Context, accountID, issuer string) (TwoFactorSetupModel, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	value, err := a.repository.BeginTwoFactor(mergedCtx, strings.TrimSpace(accountID), strings.TrimSpace(issuer))
	return TwoFactorSetupModel{Secret: value.Secret, URI: value.URI}, err
}

func (a *Admin) ConfirmTwoFactor(ctx context.Context, accountID, code string) ([]string, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	return a.repository.ConfirmTwoFactor(mergedCtx, strings.TrimSpace(accountID), code, time.Now())
}

func (a *Admin) DisableTwoFactor(ctx context.Context, accountID, code string) (int64, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	return a.repository.DisableTwoFactor(mergedCtx, strings.TrimSpace(accountID), code, time.Now())
}
