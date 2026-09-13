package wireguard

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// writeTestKey writes a usable OpenSSH private key and returns its path along
// with the matching public key.
func writeTestKey(t *testing.T) (path string, pub ssh.PublicKey) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	path = t.TempDir() + "/id_ed25519"
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.ParsePrivateKey(pem.EncodeToMemory(block))
	if err != nil {
		t.Fatal(err)
	}
	return path, signer.PublicKey()
}

func TestNewSSHRunnerKeyErrors(t *testing.T) {
	if _, err := NewSSHRunner("host:22", "root", "/no/such/key", ""); err == nil ||
		!strings.Contains(err.Error(), "read ssh key") {
		t.Errorf("missing key: err = %v", err)
	}

	bad := t.TempDir() + "/garbage"
	if err := os.WriteFile(bad, []byte("not a key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSSHRunner("host:22", "root", bad, ""); err == nil ||
		!strings.Contains(err.Error(), "parse ssh key") {
		t.Errorf("malformed key: err = %v", err)
	}
}

// With no known_hosts configured the runner still builds — the WireGuard
// integration has always worked this way — but it is unverified.
func TestNewSSHRunnerWithoutKnownHosts(t *testing.T) {
	key, _ := writeTestKey(t)
	r, err := NewSSHRunner("host:22", "root", key, "")
	if err != nil {
		t.Fatal(err)
	}
	if r.config.HostKeyCallback == nil {
		t.Fatal("no host key callback")
	}
	// Unverified means exactly that: any key is accepted.
	_, pub := writeTestKey(t)
	if err := r.config.HostKeyCallback("host:22", fakeAddr{}, pub); err != nil {
		t.Errorf("unverified callback rejected a key: %v", err)
	}
}

// A configured known_hosts file must actually be enforced: the pinned key is
// accepted and any other key is refused.
func TestNewSSHRunnerVerifiesAgainstKnownHosts(t *testing.T) {
	key, pub := writeTestKey(t)
	_, otherPub := writeTestKey(t)

	kh := t.TempDir() + "/known_hosts"
	line := knownHostsLine("host", pub)
	if err := os.WriteFile(kh, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}

	r, err := NewSSHRunner("host:22", "root", key, kh)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.config.HostKeyCallback("host:22", fakeAddr{}, pub); err != nil {
		t.Errorf("the pinned key was rejected: %v", err)
	}
	if err := r.config.HostKeyCallback("host:22", fakeAddr{}, otherPub); err == nil {
		t.Error("an unpinned key was accepted; the connection is not verified")
	}
}

func TestNewSSHRunnerRejectsUnreadableKnownHosts(t *testing.T) {
	key, _ := writeTestKey(t)
	_, err := NewSSHRunner("host:22", "root", key, "/no/such/known_hosts")
	if err == nil || !strings.Contains(err.Error(), "known_hosts") {
		t.Errorf("err = %v, want a known_hosts read failure", err)
	}
}

func knownHostsLine(host string, pub ssh.PublicKey) string {
	return host + " " + pub.Type() + " " +
		strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub))[len(pub.Type())+1:]) + "\n"
}

type fakeAddr struct{}

func (fakeAddr) Network() string { return "tcp" }
func (fakeAddr) String() string  { return "127.0.0.1:22" }
