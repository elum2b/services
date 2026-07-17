package repository

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
)

const encryptedSecretPrefix = "v1:"

func (r *Repository) encryptSecret(value string) (string, error) {
	aead, err := r.secretAEAD()
	if err != nil {
		return "", err
	}

	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("control secret nonce generation failed: %w", err)
	}

	sealed := aead.Seal(nonce, nonce, []byte(value), nil)
	return encryptedSecretPrefix + base64.RawURLEncoding.EncodeToString(sealed), nil
}

func (r *Repository) decryptSecret(value string) (string, error) {
	aead, err := r.secretAEAD()
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(value, encryptedSecretPrefix) {
		return "", fmt.Errorf("control encrypted secret has unsupported format")
	}

	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, encryptedSecretPrefix))
	if err != nil {
		return "", fmt.Errorf("control encrypted secret decode failed: %w", err)
	}
	if len(raw) < aead.NonceSize() {
		return "", fmt.Errorf("control encrypted secret is truncated")
	}

	nonce := raw[:aead.NonceSize()]
	plain, err := aead.Open(nil, nonce, raw[aead.NonceSize():], nil)
	if err != nil {
		return "", fmt.Errorf("control encrypted secret authentication failed: %w", err)
	}

	return string(plain), nil
}

func (r *Repository) secretAEAD() (cipher.AEAD, error) {
	if len(r.secretEncryptionKey) != 32 {
		return nil, ErrSecretEncryptionKey
	}

	block, err := aes.NewCipher(r.secretEncryptionKey)
	if err != nil {
		return nil, fmt.Errorf("control secret cipher initialization failed: %w", err)
	}

	return cipher.NewGCM(block)
}
