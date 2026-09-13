package store

import (
	"strings"
	"testing"
)

func testKey() []byte {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	return key
}

func TestSecretSettingsAreEncryptedAtRest(t *testing.T) {
	eachDialectWithKey(t, testKey(), func(t *testing.T, s *Store) {
		if err := s.SetSetting(t.Context(), "pihole_password", "hunter2"); err != nil {
			t.Fatal(err)
		}
		raw, err := s.rawSetting(t.Context(), "pihole_password")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(raw, encPrefix) {
			t.Fatalf("stored value is not marked encrypted: %q", raw)
		}
		if strings.Contains(raw, "hunter2") {
			t.Fatalf("plaintext survives in the stored value: %q", raw)
		}
		got, err := s.GetSetting(t.Context(), "pihole_password")
		if err != nil {
			t.Fatal(err)
		}
		if got != "hunter2" {
			t.Fatalf("GetSetting = %q, want hunter2", got)
		}

		// Configuration that is not a credential stays readable, so the table
		// is still worth opening with a SQL client.
		if err := s.SetSetting(t.Context(), "pihole_url", "http://pi.hole"); err != nil {
			t.Fatal(err)
		}
		if raw, _ := s.rawSetting(t.Context(), "pihole_url"); raw != "http://pi.hole" {
			t.Fatalf("non-secret setting stored as %q", raw)
		}
	})
}

// Every deployment written before encryption existed holds plaintext, and a
// value never re-entered would stay that way.
func TestPlaintextSecretsStillReadAndGetEncrypted(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *Store) {
		if err := s.SetSetting(t.Context(), "proxmox_secret", "legacy-token"); err != nil {
			t.Fatal(err)
		}
		// Same database, now with a key configured.
		s.crypter = mustCrypter(t, testKey())

		if got, err := s.GetSetting(t.Context(), "proxmox_secret"); err != nil || got != "legacy-token" {
			t.Fatalf("plaintext read back as %q err=%v", got, err)
		}
		n, err := s.EncryptExistingSecrets(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("re-encrypted %d rows, want 1", n)
		}
		if raw, _ := s.rawSetting(t.Context(), "proxmox_secret"); !strings.HasPrefix(raw, encPrefix) {
			t.Fatalf("row not encrypted: %q", raw)
		}
		if got, err := s.GetSetting(t.Context(), "proxmox_secret"); err != nil || got != "legacy-token" {
			t.Fatalf("re-encrypted value reads back as %q err=%v", got, err)
		}
		// Running again changes nothing.
		if n, err := s.EncryptExistingSecrets(t.Context()); err != nil || n != 0 {
			t.Fatalf("second pass: n=%d err=%v", n, err)
		}
	})
}

// A wrong or missing key must be reported. Returning "" would look exactly like
// an integration whose credential was never configured.
func TestEncryptedSecretWithoutTheRightKeyIsAnError(t *testing.T) {
	eachDialectWithKey(t, testKey(), func(t *testing.T, s *Store) {
		if err := s.SetSetting(t.Context(), "proxmox_secret", "token"); err != nil {
			t.Fatal(err)
		}

		s.crypter = nil
		if _, err := s.GetSetting(t.Context(), "proxmox_secret"); err == nil {
			t.Error("reading an encrypted setting with no key should fail")
		}

		other := testKey()
		other[0] ^= 0xff
		s.crypter = mustCrypter(t, other)
		if _, err := s.GetSetting(t.Context(), "proxmox_secret"); err == nil {
			t.Error("reading an encrypted setting with the wrong key should fail")
		}
	})
}

// The setting name is authenticated, so a ciphertext moved to another row does
// not decrypt there: no redirecting the Proxmox token into a field netis sends
// somewhere else.
func TestSecretCiphertextIsBoundToItsSettingName(t *testing.T) {
	eachDialectWithKey(t, testKey(), func(t *testing.T, s *Store) {
		if err := s.SetSetting(t.Context(), "proxmox_secret", "token"); err != nil {
			t.Fatal(err)
		}
		raw, err := s.rawSetting(t.Context(), "proxmox_secret")
		if err != nil {
			t.Fatal(err)
		}
		// Write that exact ciphertext into the other secret row.
		if _, err := s.exec(t.Context(), `INSERT INTO setting (key,value) VALUES (?,?)
			ON CONFLICT(key) DO UPDATE SET value=excluded.value`, "pihole_password", raw); err != nil {
			t.Fatal(err)
		}
		if _, err := s.GetSetting(t.Context(), "pihole_password"); err == nil {
			t.Error("a ciphertext moved to another setting should not decrypt")
		}
	})
}

func TestOpenRejectsABadSecretKey(t *testing.T) {
	if _, err := Open(":memory:", Options{SecretKey: []byte("too-short")}); err == nil {
		t.Fatal("Open should reject a key AES cannot use")
	}
}

func mustCrypter(t *testing.T, key []byte) *crypter {
	t.Helper()
	c, err := newCrypter(key)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
