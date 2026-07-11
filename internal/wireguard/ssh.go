package wireguard

import (
	"context"
	"fmt"
	"os"
	"time"

	"golang.org/x/crypto/ssh"
)

type Runner interface {
	Run(ctx context.Context, cmd string) ([]byte, error)
}

type SSHRunner struct {
	addr   string
	config *ssh.ClientConfig
}

func NewSSHRunner(addr, user, keyPath string) (*SSHRunner, error) {
	key, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("read ssh key: %w", err)
	}
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("parse ssh key: %w", err)
	}
	return &SSHRunner{
		addr: addr,
		config: &ssh.ClientConfig{
			User:            user,
			Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
			HostKeyCallback: ssh.InsecureIgnoreHostKey(), // home-lab: pin later if needed
			Timeout:         10 * time.Second,
		},
	}, nil
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
	return sess.Output(cmd)
}
