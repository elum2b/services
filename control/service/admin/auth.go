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

	result, err := a.repository.CompleteAuth(
		mergedCtx,
		repository.IdentityInput{
			Provider:    strings.TrimSpace(params.Provider),
			Subject:     strings.TrimSpace(params.Subject),
			DisplayName: strings.TrimSpace(params.DisplayName),
			Payload:     params.Payload,
		},
		strings.TrimSpace(params.InviteToken),
		repository.SessionInput{
			IP:        strings.TrimSpace(params.IP),
			UserAgent: strings.TrimSpace(params.UserAgent),
			BindToIP:  params.BindToIP,
			ExpiresAt: params.ExpiresAt,
		},
	)
	if err != nil {
		return AuthResult{}, err
	}

	return AuthResult{
		Account:            mapAccount(result.Account),
		Session:            mapSession(result.Session),
		SessionToken:       result.SessionToken,
		TwoFactorRequired:  result.TwoFactorRequired,
		TwoFactorChallenge: result.TwoFactorChallenge,
		Created:            result.Created,
	}, nil
}

func (a *Admin) IsInitialized(ctx context.Context) (bool, error) {

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	return a.repository.IsInitialized(mergedCtx)

}

func (a *Admin) Initialize(ctx context.Context, params AuthIdentityParams) (AuthResult, error) {

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	result, err := a.repository.Initialize(
		mergedCtx,
		repository.IdentityInput{
			Provider:    strings.TrimSpace(params.Provider),
			Subject:     strings.TrimSpace(params.Subject),
			DisplayName: strings.TrimSpace(params.DisplayName),
			Payload:     params.Payload,
		},
		repository.SessionInput{
			IP:        strings.TrimSpace(params.IP),
			UserAgent: strings.TrimSpace(params.UserAgent),
			BindToIP:  params.BindToIP,
			ExpiresAt: params.ExpiresAt,
		},
	)
	if err != nil {
		return AuthResult{}, err
	}

	return AuthResult{
		Account:      mapAccount(result.Account),
		Session:      mapSession(result.Session),
		SessionToken: result.SessionToken,
		Created:      result.Created,
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
	return AuthResult{
		Account:      mapAccount(account),
		Session:      mapSession(session),
		SessionToken: token,
	}, nil
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

	accountID = strings.TrimSpace(accountID)
	provider := strings.TrimSpace(params.Provider)

	mergedCtx, cancel := a.withMutation(ctx, repository.AuditEvent{
		Scope:      repository.ScopeGlobal,
		ActorID:    accountID,
		MethodKey:  "control.auth.identity.bind",
		TargetType: "identity",
		TargetID:   provider,
	})
	defer cancel()

	return a.repository.BindIdentity(mergedCtx, accountID, repository.IdentityInput{
		Provider:    provider,
		Subject:     strings.TrimSpace(params.Subject),
		DisplayName: strings.TrimSpace(params.DisplayName),
		Payload:     params.Payload,
	})

}

func (a *Admin) UnbindIdentity(ctx context.Context, accountID, provider string) (int64, error) {

	accountID = strings.TrimSpace(accountID)
	provider = strings.TrimSpace(provider)

	mergedCtx, cancel := a.withMutation(ctx, repository.AuditEvent{
		Scope:      repository.ScopeGlobal,
		ActorID:    accountID,
		MethodKey:  "control.auth.identity.unbind",
		TargetType: "identity",
		TargetID:   provider,
	})
	defer cancel()

	return a.repository.UnbindIdentity(mergedCtx, accountID, provider)

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

	accountID = strings.TrimSpace(accountID)
	sessionID = strings.TrimSpace(sessionID)

	mergedCtx, cancel := a.withMutation(ctx, repository.AuditEvent{
		Scope:      repository.ScopeGlobal,
		ActorID:    accountID,
		MethodKey:  "control.auth.session.revoke",
		TargetType: "session",
		TargetID:   sessionID,
	})
	defer cancel()

	return a.repository.RevokeSession(mergedCtx, accountID, sessionID)

}

func (a *Admin) RevokeAllSessions(ctx context.Context, accountID, exceptSessionID string) (int64, error) {

	accountID = strings.TrimSpace(accountID)
	exceptSessionID = strings.TrimSpace(exceptSessionID)

	mergedCtx, cancel := a.withMutation(ctx, repository.AuditEvent{
		Scope:      repository.ScopeGlobal,
		ActorID:    accountID,
		MethodKey:  "control.auth.session.revoke_all",
		TargetType: "account",
		TargetID:   accountID,
	})
	defer cancel()

	return a.repository.RevokeAllSessions(mergedCtx, accountID, exceptSessionID)

}

func (a *Admin) BeginTwoFactor(ctx context.Context, accountID, issuer string) (TwoFactorSetupModel, error) {

	accountID = strings.TrimSpace(accountID)

	mergedCtx, cancel := a.withMutation(ctx, repository.AuditEvent{
		Scope:      repository.ScopeGlobal,
		ActorID:    accountID,
		MethodKey:  "control.auth.two_factor.begin",
		TargetType: "account",
		TargetID:   accountID,
	})
	defer cancel()

	value, err := a.repository.BeginTwoFactor(mergedCtx, accountID, strings.TrimSpace(issuer))

	return TwoFactorSetupModel{Secret: value.Secret, URI: value.URI}, err

}

func (a *Admin) ConfirmTwoFactor(ctx context.Context, accountID, code string) ([]string, error) {

	accountID = strings.TrimSpace(accountID)

	mergedCtx, cancel := a.withMutation(ctx, repository.AuditEvent{
		Scope:      repository.ScopeGlobal,
		ActorID:    accountID,
		MethodKey:  "control.auth.two_factor.confirm",
		TargetType: "account",
		TargetID:   accountID,
	})
	defer cancel()

	return a.repository.ConfirmTwoFactor(mergedCtx, accountID, code, time.Now())

}

func (a *Admin) DisableTwoFactor(ctx context.Context, accountID, code string) (int64, error) {

	accountID = strings.TrimSpace(accountID)

	mergedCtx, cancel := a.withMutation(ctx, repository.AuditEvent{
		Scope:      repository.ScopeGlobal,
		ActorID:    accountID,
		MethodKey:  "control.auth.two_factor.disable",
		TargetType: "account",
		TargetID:   accountID,
	})
	defer cancel()

	return a.repository.DisableTwoFactor(mergedCtx, accountID, code, time.Now())

}
