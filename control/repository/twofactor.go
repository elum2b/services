package repository

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"database/sql"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	controlsqlc "github.com/elum2b/services/control/sqlc"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	json "github.com/goccy/go-json"
	"github.com/google/uuid"
)

const twoFactorPeriod = 30 * time.Second

type TwoFactorSetup struct{ Secret, URI string }

func (r *Repository) BeginTwoFactor(ctx context.Context, accountID, issuer string) (TwoFactorSetup, error) {
	if err := required(accountID); err != nil {
		return TwoFactorSetup{}, err
	}
	secret, err := randomSecret()
	if err != nil {
		return TwoFactorSetup{}, err
	}
	encryptedSecret, err := r.encryptSecret(secret)
	if err != nil {
		return TwoFactorSetup{}, err
	}
	if issuer = strings.TrimSpace(issuer); issuer == "" {
		issuer = "Elum"
	}
	err = sqlwrap.WithTx(
		ctx,
		r.db.DB(),
		func(tx *sql.Tx) *controlsqlc.Queries {
			return controlsqlc.New(tx)
		},
		func(tx *sql.Tx, q *controlsqlc.Queries) error {
			if _, err := tx.ExecContext(
				ctx,
				"SELECT pg_advisory_xact_lock(hashtextextended($1, 0))",
				"control:two-factor:"+accountID,
			); err != nil {
				return err
			}

			account, err := q.GetAccount(ctx, accountID)
			if err != nil {
				return noRows(err, ErrAccountNotFound)
			}
			if account.Status != "active" {
				return ErrForbidden
			}

			active, err := q.HasActiveTwoFactor(ctx, accountID)
			if err != nil {
				return err
			}
			if active {
				return ErrTwoFactorEnabled
			}

			return q.UpsertTwoFactor(ctx, controlsqlc.UpsertTwoFactorParams{
				AccountID:    accountID,
				Secret:       encryptedSecret,
				BackupHashes: json.RawMessage(`[]`),
			})
		},
	)
	if err != nil {
		return TwoFactorSetup{}, err
	}
	uri := fmt.Sprintf(
		"otpauth://totp/%s?secret=%s&issuer=%s&period=30&digits=6",
		url.PathEscape(issuer+":"+accountID),
		url.QueryEscape(secret),
		url.QueryEscape(issuer),
	)
	return TwoFactorSetup{Secret: secret, URI: uri}, nil
}

func (r *Repository) ConfirmTwoFactor(ctx context.Context, accountID, code string, now time.Time) ([]string, error) {
	codes, hashes, err := newBackupCodes()
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(hashes)
	if err != nil {
		return nil, err
	}
	err = sqlwrap.WithTx(
		ctx,
		r.db.DB(),
		func(tx *sql.Tx) *controlsqlc.Queries { return controlsqlc.New(tx) },
		func(_ *sql.Tx, q *controlsqlc.Queries) error {
			row, err := q.GetTwoFactor(ctx, accountID)
			if err != nil {
				return noRows(err, ErrNotFound)
			}
			secret, err := r.decryptSecret(row.Secret)
			if err != nil {
				return err
			}
			if row.ActivatedAt.Valid || !validTOTP(secret, code, now) {
				return ErrForbidden
			}
			if rows, err := q.UpdatePendingTwoFactorBackupHashes(ctx, controlsqlc.UpdatePendingTwoFactorBackupHashesParams{BackupHashes: encoded, AccountID: accountID}); err != nil ||
				rows != 1 {
				if err != nil {
					return err
				}
				return ErrForbidden
			}
			if rows, err := q.ActivateTwoFactor(ctx, accountID); err != nil || rows != 1 {
				if err != nil {
					return err
				}
				return ErrForbidden
			}
			return nil
		},
	)
	if err != nil {
		return nil, err
	}
	return codes, nil
}

func (r *Repository) VerifyTwoFactor(ctx context.Context, accountID, code string, now time.Time) error {
	return sqlwrap.WithTx(
		ctx,
		r.db.DB(),
		func(tx *sql.Tx) *controlsqlc.Queries { return controlsqlc.New(tx) },
		func(_ *sql.Tx, q *controlsqlc.Queries) error {
			return r.verifyTwoFactorWithQueries(ctx, q, accountID, code, now)
		},
	)
}

func (r *Repository) CompleteTwoFactorChallenge(
	ctx context.Context,
	rawChallenge, code, ip string,
	now time.Time,
) (Session, string, error) {
	var session Session
	var rawSession string
	var rejected error
	err := sqlwrap.WithTx(
		ctx,
		r.db.DB(),
		func(tx *sql.Tx) *controlsqlc.Queries { return controlsqlc.New(tx) },
		func(_ *sql.Tx, q *controlsqlc.Queries) error {
			challenge, err := q.GetTwoFactorChallengeWithFactorForUpdate(ctx, tokenHash(rawChallenge))
			if err != nil {
				return noRows(err, ErrNotFound)
			}
			if challenge.BindToIp && challenge.Ip != ip {
				rejected = ErrForbidden
				return consumeTwoFactorChallenge(ctx, q, challenge.ChallengeID)
			}
			if err := r.verifyTwoFactorData(
				ctx,
				q,
				challenge.AccountID,
				challenge.Secret,
				challenge.BackupHashes,
				challenge.ActivatedAt,
				code,
				now,
			); err != nil {
				if errors.Is(err, ErrForbidden) {
					rejected = err
					return consumeTwoFactorChallenge(ctx, q, challenge.ChallengeID)
				}
				return err
			}

			account, err := q.GetAccount(ctx, challenge.AccountID)
			if err != nil {
				return noRows(err, ErrAccountNotFound)
			}
			if account.Status != "active" {
				rejected = ErrForbidden
				return consumeTwoFactorChallenge(ctx, q, challenge.ChallengeID)
			}
			if err := consumeTwoFactorChallenge(ctx, q, challenge.ChallengeID); err != nil {
				return err
			}
			rawSession, err = randomToken()
			if err != nil {
				return err
			}
			session = Session{
				ID:        uuid.NewString(),
				AccountID: challenge.AccountID,
				IP:        challenge.Ip,
				UserAgent: challenge.UserAgent,
				BindToIP:  challenge.BindToIp,
				ExpiresAt: challenge.SessionExpiresAt,
				CreatedAt: now,
			}
			return q.CreateSession(
				ctx,
				controlsqlc.CreateSessionParams{
					ID:        session.ID,
					AccountID: session.AccountID,
					TokenHash: tokenHash(rawSession),
					Ip:        session.IP,
					UserAgent: session.UserAgent,
					BindToIp:  session.BindToIP,
					ExpiresAt: session.ExpiresAt,
				},
			)
		},
	)
	if err != nil {
		return Session{}, "", err
	}
	if rejected != nil {
		return Session{}, "", rejected
	}
	return session, rawSession, nil
}

func consumeTwoFactorChallenge(ctx context.Context, q *controlsqlc.Queries, challengeID string) error {
	rows, err := q.DeleteTwoFactorChallenge(ctx, challengeID)
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) verifyTwoFactorWithQueries(
	ctx context.Context,
	q *controlsqlc.Queries,
	accountID, code string,
	now time.Time,
) error {
	row, err := q.GetTwoFactorForUpdate(ctx, accountID)
	if err != nil {
		return noRows(err, ErrNotFound)
	}
	return r.verifyTwoFactorData(
		ctx,
		q,
		accountID,
		row.Secret,
		row.BackupHashes,
		row.ActivatedAt,
		code,
		now,
	)
}

func (r *Repository) verifyTwoFactorData(
	ctx context.Context,
	q *controlsqlc.Queries,
	accountID, secret string,
	backupHashes json.RawMessage,
	activatedAt sql.NullTime,
	code string,
	now time.Time,
) error {
	if !activatedAt.Valid {
		return ErrForbidden
	}
	secret, err := r.decryptSecret(secret)
	if err != nil {
		return err
	}
	if validTOTP(secret, code, now) {
		return nil
	}
	var hashes []string
	if err := json.Unmarshal(backupHashes, &hashes); err != nil {
		return err
	}
	needle := backupHash(code)
	index := -1
	for i, hash := range hashes {
		if hmac.Equal([]byte(hash), []byte(needle)) {
			index = i
			break
		}
	}
	if index < 0 {
		return ErrForbidden
	}
	hashes = append(hashes[:index], hashes[index+1:]...)
	encoded, err := json.Marshal(hashes)
	if err != nil {
		return err
	}
	rows, err := q.UpdateTwoFactorBackupHashes(
		ctx,
		controlsqlc.UpdateTwoFactorBackupHashesParams{BackupHashes: encoded, AccountID: accountID},
	)
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrForbidden
	}
	return nil
}

func (r *Repository) DisableTwoFactor(ctx context.Context, accountID, code string, now time.Time) (int64, error) {
	var rows int64
	err := sqlwrap.WithTx(
		ctx,
		r.db.DB(),
		func(tx *sql.Tx) *controlsqlc.Queries { return controlsqlc.New(tx) },
		func(_ *sql.Tx, q *controlsqlc.Queries) error {
			if err := r.verifyTwoFactorWithQueries(ctx, q, accountID, code, now); err != nil {
				return err
			}
			var err error
			rows, err = q.DeleteTwoFactor(ctx, accountID)
			return err
		},
	)
	return rows, err
}

func randomSecret() (string, error) {
	value := make([]byte, 20)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(value), nil
}

func validTOTP(secret, code string, now time.Time) bool {
	for _, offset := range []int64{-1, 0, 1} {
		if hmac.Equal(
			[]byte(totp(secret, now.Add(time.Duration(offset)*twoFactorPeriod))),
			[]byte(strings.TrimSpace(code)),
		) {
			return true
		}
	}
	return false
}

func totp(secret string, now time.Time) string {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).
		DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return ""
	}
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(now.Unix()/30))
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(counter[:])
	sum := mac.Sum(nil)
	offset := int(sum[len(sum)-1] & 0x0f)
	value := (uint32(sum[offset])&0x7f)<<24 | uint32(
		sum[offset+1],
	)<<16 | uint32(
		sum[offset+2],
	)<<8 | uint32(
		sum[offset+3],
	)
	return fmt.Sprintf("%06d", value%1_000_000)
}

func newBackupCodes() ([]string, []string, error) {
	codes, hashes := make([]string, 10), make([]string, 10)
	for i := range codes {
		value := make([]byte, 8)
		if _, err := rand.Read(value); err != nil {
			return nil, nil, err
		}
		codes[i] = strings.ToUpper(hex.EncodeToString(value[:4])) + "-" + strings.ToUpper(hex.EncodeToString(value[4:]))
		hashes[i] = backupHash(codes[i])
	}
	return codes, hashes, nil
}

func backupHash(value string) string {
	sum := sha256.Sum256([]byte(strings.ToUpper(strings.TrimSpace(value))))
	return hex.EncodeToString(sum[:])
}
