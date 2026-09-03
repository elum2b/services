package repository

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
)

const partnerSecretPrefix = "v1:"

func (r *Repository) encryptPartnerSecret(value string) (string, error) {
	if value == "" {
		return "", nil
	}

	aead, err := r.partnerSecretAEAD()
	if err != nil {
		return "", err
	}

	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf(
			"tasks partner secret nonce generation failed: %w",
			err,
		)
	}

	sealed := aead.Seal(nonce, nonce, []byte(value), nil)

	return partnerSecretPrefix + base64.RawURLEncoding.EncodeToString(
		sealed,
	), nil
}

func (r *Repository) decryptPartnerSecret(value *string) (*string, error) {
	if value == nil || *value == "" {
		//nolint:nilnil // Missing optional secret is a valid decoded value.
		return nil, nil
	}

	if !strings.HasPrefix(*value, partnerSecretPrefix) {
		return nil, fmt.Errorf("tasks partner secret migration is required")
	}

	aead, err := r.partnerSecretAEAD()
	if err != nil {
		return nil, err
	}

	raw, err := base64.RawURLEncoding.DecodeString(
		strings.TrimPrefix(*value, partnerSecretPrefix),
	)
	if err != nil || len(raw) < aead.NonceSize() {
		return nil, fmt.Errorf("tasks encrypted partner secret is invalid")
	}

	plain, err := aead.Open(
		nil,
		raw[:aead.NonceSize()],
		raw[aead.NonceSize():],
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"tasks encrypted partner secret authentication failed: %w",
			err,
		)
	}

	result := string(plain)

	return &result, nil
}

// EncryptImportSecrets protects values retained in an asynchronous import job.
func (r *Repository) EncryptImportSecrets(
	values map[string]string,
) (map[string]string, error) {
	if len(values) == 0 {
		return map[string]string{}, nil
	}

	result := make(map[string]string, len(values))
	for key, value := range values {
		encrypted, err := r.encryptPartnerSecret(value)
		if err != nil {
			return nil, err
		}

		result[key] = encrypted
	}

	return result, nil
}

// DecryptImportSecrets restores values only while executing an import job.
func (r *Repository) DecryptImportSecrets(
	values map[string]string,
) (map[string]string, error) {
	if len(values) == 0 {
		return map[string]string{}, nil
	}

	result := make(map[string]string, len(values))
	for key, value := range values {
		plain, err := r.decryptPartnerSecret(&value)
		if err != nil {
			return nil, err
		}

		if plain != nil {
			result[key] = *plain
		}
	}

	return result, nil
}

func (r *Repository) partnerSecretAEAD() (cipher.AEAD, error) {
	if len(r.secretEncryptionKey) != 32 {
		return nil, fmt.Errorf(
			"tasks secret encryption key must contain 32 bytes",
		)
	}

	block, err := aes.NewCipher(r.secretEncryptionKey)
	if err != nil {
		return nil, fmt.Errorf(
			"tasks partner secret cipher initialization failed: %w",
			err,
		)
	}

	return cipher.NewGCM(block)
}

// migratePartnerSecrets converts rows written before encryption was introduced.
// It runs only during bootstrap, uses bound values, and never reads plaintext
// into caches or public models.
func (r *Repository) migratePartnerSecrets(ctx context.Context) error {
	rows, err := r.db.DB().QueryContext(ctx, `
SELECT workspace_id, provider, group_key, platform, secret, webhook_secret
FROM task_partner_config
WHERE (secret IS NOT NULL AND secret <> '' AND secret NOT LIKE 'v1:%')
   OR (webhook_secret IS NOT NULL AND webhook_secret <> '' AND webhook_secret NOT LIKE 'v1:%')`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type legacy struct {
		workspaceID, provider, groupKey, platform string
		secret, webhookSecret                     sql.NullString
	}

	items := make([]legacy, 0)

	for rows.Next() {
		var item legacy

		if err := rows.Scan(
			&item.workspaceID,
			&item.provider,
			&item.groupKey,
			&item.platform,
			&item.secret,
			&item.webhookSecret,
		); err != nil {
			return err
		}

		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return err
	}

	for _, item := range items {
		if item.secret.Valid && item.secret.String != "" &&
			!strings.HasPrefix(item.secret.String, partnerSecretPrefix) {
			encrypted, err := r.encryptPartnerSecret(item.secret.String)
			if err != nil {
				return err
			}

			item.secret.String = encrypted
		}

		if item.webhookSecret.Valid && item.webhookSecret.String != "" &&
			!strings.HasPrefix(item.webhookSecret.String, partnerSecretPrefix) {
			encrypted, err := r.encryptPartnerSecret(item.webhookSecret.String)
			if err != nil {
				return err
			}

			item.webhookSecret.String = encrypted
		}

		if _, err := r.db.DB().ExecContext(ctx, `
UPDATE task_partner_config SET secret = $1, webhook_secret = $2, updated_at = now()
WHERE workspace_id = $3 AND provider = $4 AND group_key = $5 AND platform = $6`,
			item.secret, item.webhookSecret, item.workspaceID, item.provider, item.groupKey, item.platform); err != nil {
			return err
		}
	}

	return nil
}
