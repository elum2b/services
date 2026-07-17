package repository

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"

	controlsqlc "github.com/elum2b/services/control/sqlc"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	"github.com/google/uuid"
)

type IdentityInput struct {
	Provider, Subject, DisplayName string
	Payload                        []byte
}

type SessionInput struct {
	IP, UserAgent string
	BindToIP      bool
	ExpiresAt     time.Time
}

func (r *Repository) AuthenticateIdentity(ctx context.Context, value IdentityInput) (Account, bool, error) {
	if err := required(value.Provider, value.Subject); err != nil {
		return Account{}, false, err
	}

	var result Account
	var created bool
	err := sqlwrap.WithTx(
		ctx,
		r.db.DB(),
		func(tx *sql.Tx) *controlsqlc.Queries {
			return controlsqlc.New(tx)
		},
		func(tx *sql.Tx, q *controlsqlc.Queries) error {
			if _, err := tx.ExecContext(
				ctx,
				"SELECT pg_advisory_xact_lock(hashtextextended($1, 0))",
				"control:identity:"+tokenHash(value.Provider+"\x00"+value.Subject),
			); err != nil {
				return err
			}

			account, err := q.FindAccountByIdentity(ctx, controlsqlc.FindAccountByIdentityParams{
				Provider:        value.Provider,
				ProviderSubject: value.Subject,
			})
			if err == nil {
				result = mapAccountRow(account)
				return nil
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}

			accountID := uuid.NewString()
			if err := q.CreateAccount(ctx, controlsqlc.CreateAccountParams{
				ID:          accountID,
				DisplayName: value.DisplayName,
			}); err != nil {
				return err
			}
			if err := q.UpsertIdentity(ctx, controlsqlc.UpsertIdentityParams{
				AccountID:       accountID,
				Provider:        value.Provider,
				ProviderSubject: value.Subject,
				Payload:         rawMessageParam(value.Payload),
			}); err != nil {
				return err
			}

			account, err = q.GetAccount(ctx, accountID)
			if err != nil {
				return err
			}
			result = mapAccountRow(account)
			created = true
			return nil
		},
	)
	if err != nil {
		return Account{}, false, err
	}
	if result.Status != "active" {
		return Account{}, false, ErrForbidden
	}
	return result, created, nil
}

func (r *Repository) BindIdentity(ctx context.Context, accountID string, value IdentityInput) error {
	if err := required(accountID, value.Provider, value.Subject); err != nil {
		return err
	}
	if _, err := r.GetAccount(ctx, accountID); err != nil {
		return err
	}
	return r.q.UpsertIdentity(
		ctx,
		controlsqlc.UpsertIdentityParams{
			AccountID:       accountID,
			Provider:        value.Provider,
			ProviderSubject: value.Subject,
			Payload:         rawMessageParam(value.Payload),
		},
	)
}

func (r *Repository) ListIdentities(ctx context.Context, accountID string) ([]Identity, error) {
	rows, err := r.q.ListIdentities(ctx, accountID)
	if err != nil {
		return nil, err
	}
	result := make([]Identity, 0, len(rows))
	for _, row := range rows {
		result = append(
			result,
			Identity{
				AccountID:       row.AccountID,
				Provider:        row.Provider,
				ProviderSubject: row.ProviderSubject,
				CreatedAt:       row.CreatedAt,
				UpdatedAt:       row.UpdatedAt,
			},
		)
	}
	return result, nil
}

func (r *Repository) UnbindIdentity(ctx context.Context, accountID, provider string) (int64, error) {
	if err := required(accountID, provider); err != nil {
		return 0, err
	}
	var rows int64
	err := sqlwrap.WithTx(
		ctx,
		r.db.DB(),
		func(tx *sql.Tx) *controlsqlc.Queries {
			return controlsqlc.New(tx)
		},
		func(tx *sql.Tx, q *controlsqlc.Queries) error {
			if _, err := tx.ExecContext(
				ctx,
				"SELECT pg_advisory_xact_lock(hashtextextended($1, 0))",
				"control:account-identities:"+accountID,
			); err != nil {
				return err
			}

			count, err := q.CountIdentities(ctx, accountID)
			if err != nil {
				return err
			}
			if count <= 1 {
				return ErrForbidden
			}

			rows, err = q.DeleteIdentity(ctx, controlsqlc.DeleteIdentityParams{
				AccountID: accountID,
				Provider:  provider,
			})
			return err
		},
	)
	return rows, err
}

func mapAccountRow(value controlsqlc.ControlAccount) Account {
	return Account{
		ID:          value.ID,
		DisplayName: value.DisplayName,
		Status:      value.Status,
		CreatedAt:   value.CreatedAt,
		UpdatedAt:   value.UpdatedAt,
	}
}

func (r *Repository) CreateSession(ctx context.Context, accountID string, value SessionInput) (Session, string, error) {
	if err := required(accountID); err != nil {
		return Session{}, "", err
	}
	if value.ExpiresAt.IsZero() {
		value.ExpiresAt = time.Now().Add(30 * 24 * time.Hour)
	}
	rawToken, err := randomToken()
	if err != nil {
		return Session{}, "", err
	}
	id := uuid.NewString()
	if err := r.q.CreateSession(ctx, controlsqlc.CreateSessionParams{ID: id, AccountID: accountID, TokenHash: tokenHash(rawToken), Ip: value.IP, UserAgent: value.UserAgent, BindToIp: value.BindToIP, ExpiresAt: value.ExpiresAt}); err != nil {
		return Session{}, "", err
	}
	return Session{
		ID:        id,
		AccountID: accountID,
		IP:        value.IP,
		UserAgent: value.UserAgent,
		BindToIP:  value.BindToIP,
		ExpiresAt: value.ExpiresAt,
		CreatedAt: time.Now(),
	}, rawToken, nil
}

func (r *Repository) CreateTwoFactorChallenge(
	ctx context.Context,
	accountID string,
	value SessionInput,
) (string, error) {
	if err := required(accountID); err != nil {
		return "", err
	}

	if value.ExpiresAt.IsZero() {
		value.ExpiresAt = time.Now().Add(30 * 24 * time.Hour)
	}

	rawToken, err := randomToken()
	if err != nil {
		return "", err
	}
	if err := r.q.CreateTwoFactorChallenge(ctx, controlsqlc.CreateTwoFactorChallengeParams{
		ID:               uuid.NewString(),
		AccountID:        accountID,
		TokenHash:        tokenHash(rawToken),
		Ip:               value.IP,
		UserAgent:        value.UserAgent,
		BindToIp:         value.BindToIP,
		ExpiresAt:        time.Now().Add(10 * time.Minute),
		SessionExpiresAt: value.ExpiresAt,
	}); err != nil {
		return "", err
	}
	return rawToken, nil
}

func (r *Repository) RequiresTwoFactor(ctx context.Context, accountID string) (bool, error) {
	return r.q.HasActiveTwoFactor(ctx, accountID)
}

func (r *Repository) ValidateSession(ctx context.Context, rawToken, ip string) (Session, error) {
	row, err := r.q.GetActiveSessionByHash(ctx, tokenHash(rawToken))
	if err != nil {
		return Session{}, noRows(err, ErrNotFound)
	}
	if row.BindToIp && row.Ip != ip {
		return Session{}, ErrForbidden
	}
	account, err := r.GetAccount(ctx, row.AccountID)
	if err != nil {
		return Session{}, err
	}
	if account.Status != "active" {
		return Session{}, ErrForbidden
	}
	_, _ = r.q.TouchSession(ctx, row.ID)
	return mapSession(row), nil
}

func (r *Repository) ListSessions(ctx context.Context, accountID string) ([]Session, error) {
	rows, err := r.q.ListSessions(ctx, accountID)
	if err != nil {
		return nil, err
	}
	result := make([]Session, 0, len(rows))
	for _, row := range rows {
		result = append(result, mapSession(row))
	}
	return result, nil
}

func (r *Repository) RevokeSession(ctx context.Context, accountID, sessionID string) (int64, error) {
	return r.q.RevokeSession(ctx, controlsqlc.RevokeSessionParams{AccountID: accountID, ID: sessionID})
}

func (r *Repository) RevokeAllSessions(ctx context.Context, accountID, exceptSessionID string) (int64, error) {
	if err := required(accountID); err != nil {
		return 0, err
	}
	return r.q.RevokeAllSessions(
		ctx,
		controlsqlc.RevokeAllSessionsParams{AccountID: accountID, Column2: exceptSessionID, ID: exceptSessionID},
	)
}

func tokenHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func randomToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func mapSession(value controlsqlc.ControlSession) Session {
	result := Session{
		ID:         value.ID,
		AccountID:  value.AccountID,
		IP:         value.Ip,
		UserAgent:  value.UserAgent,
		BindToIP:   value.BindToIp,
		ExpiresAt:  value.ExpiresAt,
		LastUsedAt: value.LastUsedAt,
		CreatedAt:  value.CreatedAt,
	}
	if value.RevokedAt.Valid {
		result.RevokedAt = &value.RevokedAt.Time
	}
	return result
}
