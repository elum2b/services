package admin

import (
	"context"
	"strings"
	"time"

	"github.com/elum2b/services/control/repository"
	"github.com/google/uuid"
)

func (a *Admin) CreateMCPToken(ctx context.Context, params CreateMCPTokenParams) (CreateMCPTokenResult, error) {

	tokenID := uuid.NewString()
	mergedCtx, cancel := a.withMutation(ctx, repository.AuditEvent{
		Scope:      repository.ScopeGlobal,
		ActorID:    strings.TrimSpace(params.AccountID),
		MethodKey:  "control.auth.mcp_token.create",
		TargetType: "mcp_token",
		TargetID:   tokenID,
	})
	defer cancel()

	expiresAt, err := mcpTokenExpiresAt(params.Lifetime, time.Now().UTC())
	if err != nil {
		return CreateMCPTokenResult{}, err
	}
	token, rawToken, err := a.repository.CreateMCPToken(
		mergedCtx,
		strings.TrimSpace(params.AccountID),
		repository.MCPTokenInput{
			ID:        tokenID,
			Name:      strings.TrimSpace(params.Name),
			ExpiresAt: expiresAt,
		},
	)
	if err != nil {
		return CreateMCPTokenResult{}, err
	}

	return CreateMCPTokenResult{
		Token:    mapMCPToken(token),
		RawToken: rawToken,
	}, nil
}

func (a *Admin) ListMCPTokens(ctx context.Context, params ListMCPTokensParams) ([]MCPTokenModel, error) {

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	values, err := a.repository.ListMCPTokens(mergedCtx, strings.TrimSpace(params.AccountID))
	if err != nil {
		return nil, err
	}
	result := make([]MCPTokenModel, 0, len(values))
	for _, value := range values {
		result = append(result, mapMCPToken(value))
	}
	return result, nil
}

func (a *Admin) RevokeMCPToken(ctx context.Context, params RevokeMCPTokenParams) (int64, error) {

	mergedCtx, cancel := a.withMutation(ctx, repository.AuditEvent{
		Scope:      repository.ScopeGlobal,
		ActorID:    strings.TrimSpace(params.AccountID),
		MethodKey:  "control.auth.mcp_token.revoke",
		TargetType: "mcp_token",
		TargetID:   strings.TrimSpace(params.TokenID),
	})
	defer cancel()

	return a.repository.RevokeMCPToken(
		mergedCtx,
		strings.TrimSpace(params.AccountID),
		strings.TrimSpace(params.TokenID),
	)
}

func mcpTokenExpiresAt(value MCPTokenLifetime, now time.Time) (*time.Time, error) {

	switch value.Kind {
	case MCPTokenLifetimeNever:
		if value.Amount != 0 {
			return nil, repository.ErrInvalidArgument
		}
		return nil, nil
	case MCPTokenLifetimeDays:
		if value.Amount < 1 || value.Amount > 36500 {
			return nil, repository.ErrInvalidArgument
		}
		expiresAt := now.AddDate(0, 0, int(value.Amount))
		return &expiresAt, nil
	case MCPTokenLifetimeMonths:
		if value.Amount < 1 || value.Amount > 1200 {
			return nil, repository.ErrInvalidArgument
		}
		expiresAt := now.AddDate(0, int(value.Amount), 0)
		return &expiresAt, nil
	default:
		return nil, repository.ErrInvalidArgument
	}
}
