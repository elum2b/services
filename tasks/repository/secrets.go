package repository

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
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
		return "", fmt.Errorf("tasks partner secret nonce generation failed: %w", err)
	}
	sealed := aead.Seal(nonce, nonce, []byte(value), nil)
	return partnerSecretPrefix + base64.RawURLEncoding.EncodeToString(sealed), nil

}

func (r *Repository) decryptPartnerSecret(value *string) (*string, error) {

	if value == nil || *value == "" {
		return nil, nil
	}
	if !strings.HasPrefix(*value, partnerSecretPrefix) {
		return nil, fmt.Errorf("tasks partner secret migration is required")
	}
	aead, err := r.partnerSecretAEAD()
	if err != nil {
		return nil, err
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(*value, partnerSecretPrefix))
	if err != nil || len(raw) < aead.NonceSize() {
		return nil, fmt.Errorf("tasks encrypted partner secret is invalid")
	}
	plain, err := aead.Open(nil, raw[:aead.NonceSize()], raw[aead.NonceSize():], nil)
	if err != nil {
		return nil, fmt.Errorf("tasks encrypted partner secret authentication failed: %w", err)
	}
	result := string(plain)
	return &result, nil

}

func (r *Repository) partnerSecretAEAD() (cipher.AEAD, error) {

	if len(r.secretEncryptionKey) != 32 {
		return nil, fmt.Errorf("tasks secret encryption key must contain 32 bytes")
	}
	block, err := aes.NewCipher(r.secretEncryptionKey)
	if err != nil {
		return nil, fmt.Errorf("tasks partner secret cipher initialization failed: %w", err)
	}
	return cipher.NewGCM(block)

}

// migratePartnerSecrets converts rows written before encryption was introduced.
// It runs only during bootstrap, uses bound values, and never reads plaintext
// into caches or public models.
func (r *Repository) migratePartnerSecrets(ctx context.Context) error {

	rows, err := r.db.DB().QueryContext(ctx, `
SELECT workspace_id, provider, group_key, platform, secret
FROM task_partner_config
WHERE secret IS NOT NULL AND secret <> '' AND secret NOT LIKE 'v1:%'`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type legacy struct{ workspaceID, provider, groupKey, platform, secret string }
	items := make([]legacy, 0)
	for rows.Next() {
		var item legacy
		if err := rows.Scan(&item.workspaceID, &item.provider, &item.groupKey, &item.platform, &item.secret); err != nil {
			return err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, item := range items {
		encrypted, err := r.encryptPartnerSecret(item.secret)
		if err != nil {
			return err
		}
		if _, err := r.db.DB().ExecContext(ctx, `
UPDATE task_partner_config SET secret = $1, updated_at = now()
WHERE workspace_id = $2 AND provider = $3 AND group_key = $4 AND platform = $5`,
			encrypted, item.workspaceID, item.provider, item.groupKey, item.platform); err != nil {
			return err
		}
	}
	return nil

}
