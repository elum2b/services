package repository

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"sort"
	"time"

	controlmodel "github.com/elum2b/services/control/model"
	controlsqlc "github.com/elum2b/services/control/sqlc"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	"github.com/google/uuid"
)

type IdentityInput struct {
	Provider    string
	Subject     string
	DisplayName string
	Payload     []byte
}

type SessionInput struct {
	IP        string
	UserAgent string
	BindToIP  bool
	ExpiresAt time.Time
}

func (r *Repository) IsInitialized(ctx context.Context) (bool, error) {

	_, err := r.q.GetPlatform(ctx)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}

	return false, err

}

func (r *Repository) Initialize(
	ctx context.Context,
	identity IdentityInput,
	metadata SessionInput,
) (AuthCompletion, error) {

	if err := required(identity.Provider, identity.Subject); err != nil {
		return AuthCompletion{}, err
	}

	var result AuthCompletion
	err := sqlwrap.WithTx(
		ctx,
		r.db.DB(),
		func(tx *sql.Tx) *controlsqlc.Queries {
			return controlsqlc.New(tx)
		},
		func(tx *sql.Tx, q *controlsqlc.Queries) error {
			if err := q.LockInitialization(ctx); err != nil {
				return err
			}

			if _, err := q.GetPlatform(ctx); err == nil {
				return ErrAlreadyInitialized
			} else if !errors.Is(err, sql.ErrNoRows) {
				return err
			}

			if err := lockIdentity(ctx, tx, identity.Provider, identity.Subject); err != nil {
				return err
			}

			if _, err := q.FindAuthPrincipalByIdentity(
				ctx,
				controlsqlc.FindAuthPrincipalByIdentityParams{
					Provider:        identity.Provider,
					ProviderSubject: identity.Subject,
				},
			); err == nil {
				return ErrAlreadyInitialized
			} else if !errors.Is(err, sql.ErrNoRows) {
				return err
			}

			account := Account{
				ID:          uuid.NewString(),
				DisplayName: identity.DisplayName,
				Status:      controlmodel.AccountStatusActive,
				CreatedAt:   time.Now(),
				UpdatedAt:   time.Now(),
			}
			if err := q.CreateAccount(ctx, controlsqlc.CreateAccountParams{
				ID:          account.ID,
				DisplayName: account.DisplayName,
			}); err != nil {
				return err
			}

			if err := q.UpsertIdentity(ctx, controlsqlc.UpsertIdentityParams{
				AccountID:       account.ID,
				Provider:        identity.Provider,
				ProviderSubject: identity.Subject,
				Payload:         rawMessageParam(identity.Payload),
			}); err != nil {
				return err
			}

			if err := q.AddPlatformMember(ctx, controlsqlc.AddPlatformMemberParams{
				AccountID: account.ID,
				InvitedBy: sql.NullString{},
			}); err != nil {
				return err
			}

			if err := q.CreatePlatform(ctx, account.ID); err != nil {
				return err
			}

			session, token, err := createSessionWithQueries(ctx, q, account.ID, metadata)
			if err != nil {
				return err
			}

			if err := appendAudit(ctx, q, AuditEvent{
				Scope:      ScopeGlobal,
				ActorID:    account.ID,
				MethodKey:  "control.global.initialize",
				TargetType: "platform",
				TargetID:   "1",
				Result:     controlmodel.AuditResultSucceeded,
			}); err != nil {
				return err
			}

			result = AuthCompletion{
				Account:      account,
				Session:      session,
				SessionToken: token,
				Created:      true,
			}

			return nil
		},
	)
	if err != nil {
		return AuthCompletion{}, err
	}

	return result, nil

}

func (r *Repository) CompleteAuth(
	ctx context.Context,
	identity IdentityInput,
	inviteToken string,
	metadata SessionInput,
) (AuthCompletion, error) {

	if err := required(identity.Provider, identity.Subject); err != nil {
		return AuthCompletion{}, err
	}

	var result AuthCompletion
	err := sqlwrap.WithTx(
		ctx,
		r.db.DB(),
		func(tx *sql.Tx) *controlsqlc.Queries {
			return controlsqlc.New(tx)
		},
		func(tx *sql.Tx, q *controlsqlc.Queries) error {
			if _, err := q.GetPlatform(ctx); err != nil {
				return noRows(err, ErrNotInitialized)
			}

			if err := lockIdentity(ctx, tx, identity.Provider, identity.Subject); err != nil {
				return err
			}

			principal, err := q.FindAuthPrincipalByIdentity(
				ctx,
				controlsqlc.FindAuthPrincipalByIdentityParams{
					Provider:        identity.Provider,
					ProviderSubject: identity.Subject,
				},
			)
			if errors.Is(err, sql.ErrNoRows) {
				return r.registerFromInvite(
					ctx,
					q,
					identity,
					inviteToken,
					metadata,
					&result,
				)
			}
			if err != nil {
				return err
			}
			if err := lockAccountAuthentication(ctx, tx, principal.ID); err != nil {
				return err
			}

			accountRow, err := q.GetAccount(ctx, principal.ID)
			if err != nil {
				return noRows(err, ErrAccountNotFound)
			}
			if accountRow.Status != string(controlmodel.AccountStatusActive) {
				return ErrForbidden
			}

			platformMember, err := q.GetPlatformMember(ctx, principal.ID)
			if err != nil {
				return noRows(err, ErrForbidden)
			}

			twoFactorEnabled, err := q.HasActiveTwoFactor(ctx, principal.ID)
			if err != nil {
				return err
			}
			account := Account{
				ID:          accountRow.ID,
				DisplayName: accountRow.DisplayName,
				Status:      controlmodel.AccountStatus(accountRow.Status),
				CreatedAt:   accountRow.CreatedAt,
				UpdatedAt:   accountRow.UpdatedAt,
			}

			var (
				inviteID       string
				inviteKind     InviteKind
				invitedBy      string
				inviteToAccept *controlsqlc.ControlInvite
			)
			if inviteToken != "" {
				invite, err := getInviteByHashForAcceptance(
					ctx,
					q,
					tokenHash(inviteToken),
				)
				if err != nil {
					return noRows(err, ErrInviteUnavailable)
				}
				if invite.AcceptedBy.Valid {
					if invite.AcceptedBy.String != account.ID {
						return ErrInviteUnavailable
					}
				} else {
					if err := validateInvite(invite, time.Now()); err != nil {
						return err
					}
					inviteID = invite.ID
					inviteKind = InviteKind(invite.Kind)
					invitedBy = invite.CreatedBy
					inviteToAccept = &invite
				}
			}
			reactivatePlatformMember := platformMember.Status != string(controlmodel.MembershipStatusActive)
			if reactivatePlatformMember {
				if inviteID == "" || inviteKind != InviteKindGlobal {
					return ErrInviteRequired
				}
			}

			if twoFactorEnabled {
				challenge, err := createTwoFactorChallengeWithQueries(
					ctx,
					q,
					account.ID,
					inviteID,
					metadata,
				)
				if err != nil {
					return err
				}

				result = AuthCompletion{
					Account:            account,
					TwoFactorRequired:  true,
					TwoFactorChallenge: challenge,
				}

				return nil
			}
			if reactivatePlatformMember {
				if err := q.AddPlatformMember(ctx, controlsqlc.AddPlatformMemberParams{
					AccountID: account.ID,
					InvitedBy: nullableString(invitedBy),
				}); err != nil {
					return err
				}
			}

			if inviteToAccept != nil {
				if err := r.acceptInviteRowWithQueries(
					ctx,
					q,
					*inviteToAccept,
					account.ID,
				); err != nil {
					return err
				}
			}

			session, token, err := createSessionWithQueries(ctx, q, account.ID, metadata)
			if err != nil {
				return err
			}

			result = AuthCompletion{
				Account:      account,
				Session:      session,
				SessionToken: token,
			}

			return nil
		},
	)
	if err != nil {
		return AuthCompletion{}, err
	}

	return result, nil

}

func (r *Repository) registerFromInvite(
	ctx context.Context,
	q *controlsqlc.Queries,
	identity IdentityInput,
	inviteToken string,
	metadata SessionInput,
	result *AuthCompletion,
) error {

	if inviteToken == "" {
		return ErrInviteRequired
	}

	invite, err := getInviteByHashForAcceptance(ctx, q, tokenHash(inviteToken))
	if err != nil {
		return noRows(err, ErrInviteUnavailable)
	}
	if err := validateInvite(invite, time.Now()); err != nil {
		return err
	}

	account := Account{
		ID:          uuid.NewString(),
		DisplayName: identity.DisplayName,
		Status:      controlmodel.AccountStatusActive,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := q.CreateAccount(ctx, controlsqlc.CreateAccountParams{
		ID:          account.ID,
		DisplayName: account.DisplayName,
	}); err != nil {
		return err
	}

	if err := q.UpsertIdentity(ctx, controlsqlc.UpsertIdentityParams{
		AccountID:       account.ID,
		Provider:        identity.Provider,
		ProviderSubject: identity.Subject,
		Payload:         rawMessageParam(identity.Payload),
	}); err != nil {
		return err
	}

	if err := q.AddPlatformMember(ctx, controlsqlc.AddPlatformMemberParams{
		AccountID: account.ID,
		InvitedBy: sql.NullString{
			String: invite.CreatedBy,
			Valid:  true,
		},
	}); err != nil {
		return err
	}

	if err := r.acceptInviteRowWithQueries(ctx, q, invite, account.ID); err != nil {
		return err
	}

	session, token, err := createSessionWithQueries(ctx, q, account.ID, metadata)
	if err != nil {
		return err
	}

	*result = AuthCompletion{
		Account:      account,
		Session:      session,
		SessionToken: token,
		Created:      true,
	}

	return nil

}

func lockIdentity(ctx context.Context, tx *sql.Tx, provider, subject string) error {

	_, err := tx.ExecContext(
		ctx,
		"SELECT pg_advisory_xact_lock(hashtextextended($1, 0))",
		"control:identity:"+tokenHash(provider+"\x00"+subject),
	)

	return err

}

func lockAccountAuthentication(ctx context.Context, tx *sql.Tx, accountID string) error {

	_, err := tx.ExecContext(
		ctx,
		"SELECT pg_advisory_xact_lock(hashtextextended($1, 0))",
		"control:account-authentication:"+accountID,
	)

	return err

}

func lockTwoFactorAccount(ctx context.Context, tx *sql.Tx, accountID string) error {

	_, err := tx.ExecContext(
		ctx,
		"SELECT pg_advisory_xact_lock(hashtextextended($1, 0))",
		"control:two-factor:"+accountID,
	)

	return err

}

func lockAccountIdentities(ctx context.Context, tx *sql.Tx, accountID string) error {

	_, err := tx.ExecContext(
		ctx,
		"SELECT pg_advisory_xact_lock(hashtextextended($1, 0))",
		"control:account-identities:"+accountID,
	)

	return err

}

func lockIdentitySubjects(
	ctx context.Context,
	tx *sql.Tx,
	provider string,
	subjects ...string,
) error {

	sort.Strings(subjects)
	previous := ""
	for _, subject := range subjects {
		if subject == "" || subject == previous {
			continue
		}
		if err := lockIdentity(ctx, tx, provider, subject); err != nil {
			return err
		}
		previous = subject
	}

	return nil

}

func createSessionWithQueries(
	ctx context.Context,
	q *controlsqlc.Queries,
	accountID string,
	value SessionInput,
) (Session, string, error) {

	if value.ExpiresAt.IsZero() {
		value.ExpiresAt = time.Now().Add(30 * 24 * time.Hour)
	}

	rawToken, err := randomToken()
	if err != nil {
		return Session{}, "", err
	}

	row, err := q.CreateSession(ctx, controlsqlc.CreateSessionParams{
		ID:        uuid.NewString(),
		AccountID: accountID,
		TokenHash: tokenHash(rawToken),
		Ip:        value.IP,
		UserAgent: value.UserAgent,
		BindToIp:  value.BindToIP,
		ExpiresAt: value.ExpiresAt,
	})
	if err != nil {
		return Session{}, "", err
	}

	return mapSession(row), rawToken, nil

}

func createTwoFactorChallengeWithQueries(
	ctx context.Context,
	q *controlsqlc.Queries,
	accountID string,
	inviteID string,
	value SessionInput,
) (string, error) {

	if value.ExpiresAt.IsZero() {
		value.ExpiresAt = time.Now().Add(30 * 24 * time.Hour)
	}

	rawToken, err := randomToken()
	if err != nil {
		return "", err
	}

	return rawToken, q.CreateTwoFactorChallenge(
		ctx,
		controlsqlc.CreateTwoFactorChallengeParams{
			ID:        uuid.NewString(),
			AccountID: accountID,
			InviteID: sql.NullString{
				String: inviteID,
				Valid:  inviteID != "",
			},
			TokenHash:        tokenHash(rawToken),
			Ip:               value.IP,
			UserAgent:        value.UserAgent,
			BindToIp:         value.BindToIP,
			ExpiresAt:        time.Now().Add(10 * time.Minute),
			SessionExpiresAt: value.ExpiresAt,
		},
	)

}

func (r *Repository) BindIdentity(ctx context.Context, accountID string, value IdentityInput) error {

	if err := required(accountID, value.Provider, value.Subject); err != nil {
		return err
	}
	return r.withAuditDBTx(ctx, func(tx *sql.Tx, q *controlsqlc.Queries) error {
		if err := lockAccountIdentities(ctx, tx, accountID); err != nil {
			return err
		}

		current, err := q.GetIdentity(ctx, controlsqlc.GetIdentityParams{
			AccountID: accountID,
			Provider:  value.Provider,
		})
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		currentSubject := ""
		if err == nil {
			currentSubject = current.ProviderSubject
		}
		if err := lockIdentitySubjects(
			ctx,
			tx,
			value.Provider,
			currentSubject,
			value.Subject,
		); err != nil {
			return err
		}

		principal, err := q.FindAuthPrincipalByIdentity(
			ctx,
			controlsqlc.FindAuthPrincipalByIdentityParams{
				Provider:        value.Provider,
				ProviderSubject: value.Subject,
			},
		)
		if err == nil && principal.ID != accountID {
			return ErrForbidden
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
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

		return q.UpsertIdentity(ctx, controlsqlc.UpsertIdentityParams{
			AccountID:       accountID,
			Provider:        value.Provider,
			ProviderSubject: value.Subject,
			Payload:         rawMessageParam(value.Payload),
		})
	})

}

func (r *Repository) ListIdentities(ctx context.Context, accountID string) ([]Identity, error) {

	rows, err := r.q.ListIdentities(ctx, accountID)
	if err != nil {
		return nil, err
	}

	result := make([]Identity, 0, len(rows))
	for _, row := range rows {
		result = append(result, Identity{
			AccountID:       row.AccountID,
			Provider:        row.Provider,
			ProviderSubject: row.ProviderSubject,
			CreatedAt:       row.CreatedAt,
			UpdatedAt:       row.UpdatedAt,
		})
	}

	return result, nil

}

func (r *Repository) UnbindIdentity(ctx context.Context, accountID, provider string) (int64, error) {

	if err := required(accountID, provider); err != nil {
		return 0, err
	}

	var rows int64
	err := r.withAuditDBTx(
		ctx,
		func(tx *sql.Tx, q *controlsqlc.Queries) error {
			if err := lockAccountIdentities(ctx, tx, accountID); err != nil {
				return err
			}

			identity, err := q.GetIdentity(ctx, controlsqlc.GetIdentityParams{
				AccountID: accountID,
				Provider:  provider,
			})
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			if err != nil {
				return err
			}
			if err := lockIdentitySubjects(
				ctx,
				tx,
				provider,
				identity.ProviderSubject,
			); err != nil {
				return err
			}
			if err := lockAccountAuthentication(ctx, tx, accountID); err != nil {
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

func (r *Repository) GetAccount(ctx context.Context, id string) (Account, error) {

	row, err := r.q.GetAccount(ctx, normalizeID(id))
	if err != nil {
		return Account{}, noRows(err, ErrAccountNotFound)
	}

	return mapAccountRow(row), nil

}

func (r *Repository) ValidateSession(ctx context.Context, rawToken, ip string) (Session, error) {

	row, err := r.q.ValidateAndTouchSession(
		ctx,
		controlsqlc.ValidateAndTouchSessionParams{
			TokenHash: tokenHash(rawToken),
			Ip:        ip,
		},
	)
	if err != nil {
		return Session{}, noRows(err, ErrNotFound)
	}

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

	var affected int64
	err := r.withAuditDBTx(ctx, func(tx *sql.Tx, q *controlsqlc.Queries) error {
		if err := lockAccountAuthentication(ctx, tx, accountID); err != nil {
			return err
		}

		var err error
		affected, err = q.RevokeSession(ctx, controlsqlc.RevokeSessionParams{
			ID:        sessionID,
			AccountID: accountID,
		})

		return err
	})

	return affected, err

}

func (r *Repository) RevokeAllSessions(
	ctx context.Context,
	accountID string,
	exceptSessionID string,
) (int64, error) {

	var affected int64
	err := r.withAuditDBTx(ctx, func(tx *sql.Tx, q *controlsqlc.Queries) error {
		if err := lockAccountAuthentication(ctx, tx, accountID); err != nil {
			return err
		}

		var err error
		affected, err = q.RevokeAllSessions(ctx, controlsqlc.RevokeAllSessionsParams{
			AccountID: accountID,
			Column2:   exceptSessionID,
		})

		return err
	})

	return affected, err

}

func mapAccountRow(value controlsqlc.ControlAccount) Account {

	return Account{
		ID:          value.ID,
		DisplayName: value.DisplayName,
		Status:      controlmodel.AccountStatus(value.Status),
		CreatedAt:   value.CreatedAt,
		UpdatedAt:   value.UpdatedAt,
	}

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

func tokenHash(value string) string {

	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])

}

func randomToken() (string, error) {

	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}

	return hex.EncodeToString(value), nil

}
