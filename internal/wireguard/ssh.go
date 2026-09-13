package wireguard

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

type Runner interface {
	Run(ctx context.Context, cmd string) ([]byte, error)
}

type SSHRunner struct {
	addr   string
	config *ssh.ClientConfig
}

// NewSSHRunner builds a runner that connects to addr as user with the private
// key at keyPath.
//
// knownHostsPath, when set, is an OpenSSH known_hosts file used to verify the
// server. Without it the server is not verified at all: anything answering on
// addr is handed the connection, and the WireGuard host's `wg show dump` output
// is read from whatever replies. That is worth a warning rather than a silent
// default, so an unverified runner says so once at construction.
func NewSSHRunner(addr, user, keyPath, knownHostsPath string) (*SSHRunner, error) {
	key, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("read ssh key: %w", err)
	}
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("parse ssh key: %w", err)
	}

	hostKey, err := hostKeyCallback(addr, knownHostsPath)
	if err != nil {
		return nil, err
	}
	return &SSHRunner{
		addr: addr,
		config: &ssh.ClientConfig{
			User:            user,
			Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
			HostKeyCallback: hostKey,
			Timeout:         10 * time.Second,
		},
	}, nil
}

// hostKeyCallback returns the verifier for the configured known_hosts file, or
// an unverified callback with a warning when none is set.
func hostKeyCallback(addr, knownHostsPath string) (ssh.HostKeyCallback, error) {
	if knownHostsPath == "" {
		slog.Warn("wireguard ssh host key is not verified; set wg_ssh_known_hosts to pin it",
			"addr", addr)
		return ssh.InsecureIgnoreHostKey(), nil
	}
	cb, err := knownhosts.New(knownHostsPath)
	if err != nil {
		return nil, fmt.Errorf("read known_hosts %s: %w", knownHostsPath, err)
	}
	return cb, nil
}

type sshResult struct {
	out []byte
	err error
}

func (r *SSHRunner) Run(ctx context.Context, cmd string) ([]byte, error) {
	client, err := ssh.Dial("tcp", r.addr, r.config)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	sess, err := client.NewSession()
	if err != nil {
		return nil, err
	}
	defer sess.Close()

	resCh := make(chan sshResult, 1)
	go func() {
		out, err := sess.Output(cmd)
		resCh <- sshResult{out: out, err: err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-resCh:
		return res.out, res.err
	}
}
