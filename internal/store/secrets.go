package store

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
)

// encPrefix marks a stored setting value as encrypted. It is versioned so a
// later scheme can be told apart from this one without guessing.
const encPrefix = "enc:v1:"

// SecretSettings names the setting keys whose values are credentials rather
// than configuration. They are encrypted at rest when a key is configured;
// everything else in the setting table is plain configuration and stays
// readable, which keeps the table useful to look at with a SQL client.
var SecretSettings = []string{"proxmox_secret", "pihole_password"}

func isSecretSetting(key string) bool {
	for _, k := range SecretSettings {
		if k == key {
			return true
		}
	}
	return false
}

// crypter seals and opens secret setting values with AES-GCM.
type crypter struct{ aead cipher.AEAD }

// newCrypter builds a crypter from a raw AES key (16, 24 or 32 bytes).
func newCrypter(key []byte) (*crypter, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secret key: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secret key: %w", err)
	}
	return &crypter{aead: aead}, nil
}

// seal encrypts value for storage under the given setting key.
//
// The key name is authenticated alongside the ciphertext, so a value copied
// from one setting row to another fails to open rather than quietly
// decrypting: someone with write access to the database cannot move the
// Proxmox token into the Pi-hole password field to have netis send it to a
// host they control.
func (c *crypter) seal(key, value string) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := c.aead.Seal(nonce, nonce, []byte(value), []byte(key))
	return encPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// open decrypts a value written by seal under the same setting key.
func (c *crypter) open(key, stored string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, encPrefix))
	if err != nil {
		return "", fmt.Errorf("setting %q: %w", key, err)
	}
	if len(raw) < c.aead.NonceSize() {
		return "", fmt.Errorf("setting %q: encrypted value is truncated", key)
	}
	nonce, ciphertext := raw[:c.aead.NonceSize()], raw[c.aead.NonceSize():]
	plain, err := c.aead.Open(nil, nonce, ciphertext, []byte(key))
	if err != nil {
		// Deliberately vague about which check failed, and deliberately loud:
		// the usual cause is the wrong key, and an integration silently
		// treating its credential as unset would look like a network fault.
		return "", fmt.Errorf("setting %q cannot be decrypted with the configured key", key)
	}
	return string(plain), nil
}

// EncryptExistingSecrets rewrites any secret setting still stored in plaintext,
// reporting how many rows it changed. It is a no-op without a key configured,
// and safe to call on every start: values already encrypted are left alone.
//
// Without this, adding NETIS_SECRET_KEY to an existing deployment would protect
// only the credentials someone happened to re-enter afterwards.
func (s *Store) EncryptExistingSecrets(ctx context.Context) (int, error) {
	if s.crypter == nil {
		return 0, nil
	}
	n := 0
	for _, key := range SecretSettings {
		raw, err := s.rawSetting(ctx, key)
		if err != nil {
			return n, err
		}
		if raw == "" || strings.HasPrefix(raw, encPrefix) {
			continue
		}
		if err := s.SetSetting(ctx, key, raw); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
