package repository

import (
	"context"
	"database/sql"
	"strings"
	"time"

	controlmodel "github.com/elum2b/services/control/model"
	controlsqlc "github.com/elum2b/services/control/sqlc"
)

type MCPTokenInput struct {
	ID        string
	Name      string
	ExpiresAt *time.Time
}

func (r *Repository) CreateMCPToken(
	ctx context.Context,
	accountID string,
	value MCPTokenInput,
) (MCPToken, string, error) {
	accountID = normalizeID(accountID)
	value.Name = strings.TrimSpace(value.Name)

	if err := required(accountID, value.ID, value.Name); err != nil {
		return MCPToken{}, "", err
	}

	if len(value.Name) > 128 ||
		(value.ExpiresAt != nil && !value.ExpiresAt.After(time.Now())) {
		return MCPToken{}, "", ErrInvalidArgument
	}

	var (
		token    MCPToken
		rawToken string
	)

	err := r.withAuditDBTx(ctx, func(tx *sql.Tx, q *controlsqlc.Queries) error {
		if err := lockAccountAuthentication(ctx, tx, accountID); err != nil {
			return err
		}

		account, err := q.GetAccount(ctx, accountID)
		if err != nil {
			return noRows(err, ErrAccountNotFound)
		}

		if account.Status != string(controlmodel.AccountStatusActive) {
			return ErrForbidden
		}

		member, err := q.GetPlatformMember(ctx, accountID)
		if err != nil {
			return noRows(err, ErrForbidden)
		}

		if member.Status != string(controlmodel.MembershipStatusActive) {
			return ErrForbidden
		}

		secret, err := randomToken()
		if err != nil {
			return err
		}

		rawToken = "mcp_" + secret

		row, err := q.CreateMCPToken(ctx, controlsqlc.CreateMCPTokenParams{
			ID:        value.ID,
			AccountID: accountID,
			Name:      value.Name,
			TokenHash: tokenHash(rawToken),
			ExpiresAt: nullableTime(value.ExpiresAt),
		})
		if err != nil {
			return err
		}

		token = mapMCPToken(row)

		return nil
	})
	if err != nil {
		return MCPToken{}, "", err
	}

	return token, rawToken, nil
}

func (r *Repository) ListMCPTokens(
	ctx context.Context,
	accountID string,
) ([]MCPToken, error) {
	if err := required(accountID); err != nil {
		return nil, err
	}

	rows, err := r.q.ListMCPTokens(ctx, normalizeID(accountID))
	if err != nil {
		return nil, err
	}

	result := make([]MCPToken, 0, len(rows))
	for _, row := range rows {
		result = append(result, mapMCPToken(row))
	}

	return result, nil
}

func (r *Repository) RevokeMCPToken(
	ctx context.Context,
	accountID, tokenID string,
) (int64, error) {
	if err := required(accountID, tokenID); err != nil {
		return 0, err
	}

	var affected int64

	err := r.withAuditDBTx(ctx, func(tx *sql.Tx, q *controlsqlc.Queries) error {
		if err := lockAccountAuthentication(
			ctx,
			tx,
			normalizeID(accountID),
		); err != nil {
			return err
		}

		var err error

		affected, err = q.RevokeMCPToken(ctx, controlsqlc.RevokeMCPTokenParams{
			ID:        normalizeID(tokenID),
			AccountID: normalizeID(accountID),
		})

		return err
	})

	return affected, err
}

func (r *Repository) ValidateMCPToken(
	ctx context.Context,
	rawToken string,
) (MCPToken, error) {
	if !strings.HasPrefix(rawToken, "mcp_") || len(rawToken) <= len("mcp_") {
		return MCPToken{}, ErrNotFound
	}

	row, err := r.q.ValidateAndTouchMCPToken(ctx, tokenHash(rawToken))
	if err != nil {
		return MCPToken{}, noRows(err, ErrNotFound)
	}

	return mapMCPToken(row), nil
}

func mapMCPToken(value controlsqlc.ControlMcpToken) MCPToken {
	result := MCPToken{
		ID:         value.ID,
		AccountID:  value.AccountID,
		Name:       value.Name,
		LastUsedAt: value.LastUsedAt,
		CreatedAt:  value.CreatedAt,
	}
	if value.ExpiresAt.Valid {
		result.ExpiresAt = &value.ExpiresAt.Time
	}

	if value.RevokedAt.Valid {
		result.RevokedAt = &value.RevokedAt.Time
	}

	return result
}
