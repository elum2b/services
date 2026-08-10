package admin

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/elum2b/services/control/repository"
)

func (a *Admin) CreateMCPToken(
	ctx context.Context,
	params CreateMCPTokenParams,
) (CreateMCPTokenResult, error) {
	tokenID := uuid.NewString()
	mergedCtx, cancel := a.withMutation(ctx, repository.AuditEvent{
		Scope:      repository.ScopeGlobal,
		ActorID:    strings.TrimSpace(params.AccountID),
		MethodKey:  "control.auth.mcp_token.create",
		TargetType: "mcp_token",
		TargetID:   tokenID,
	})

	defer cancel()

	expiresAt, err := mcpTokenExpiresAt(params.Duration, time.Now().UTC())
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

func (a *Admin) ListMCPTokens(
	ctx context.Context,
	params ListMCPTokensParams,
) ([]MCPTokenModel, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	values, err := a.repository.ListMCPTokens(
		mergedCtx,
		strings.TrimSpace(params.AccountID),
	)
	if err != nil {
		return nil, err
	}

	result := make([]MCPTokenModel, 0, len(values))
	for _, value := range values {
		result = append(result, mapMCPToken(value))
	}

	return result, nil
}

func (a *Admin) RevokeMCPToken(
	ctx context.Context,
	params RevokeMCPTokenParams,
) (int64, error) {
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

func mcpTokenExpiresAt(
	duration time.Duration,
	now time.Time,
) (*time.Time, error) {
	if duration == 0 {
		return nil, nil
	}

	if duration < 0 {
		return nil, repository.ErrInvalidArgument
	}

	expiresAt := now.Add(duration)

	return &expiresAt, nil
}
