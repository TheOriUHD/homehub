package minecraft

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// SSHConfig is the network's host credential: one login onto the machine
// running the servers, so HomeHub (and the assistant, under the caller's
// own permissions) can reach past the companions for full restarts.
type SSHConfig struct {
	Host     string
	Port     int
	User     string
	Password string // one of Password / PrivateKey
	Key      string // PEM private key
}

// RunSSH executes one command over the network's SSH credential and
// returns combined output. Non-zero exits return the output AND an error
// so callers can show both.
func RunSSH(ctx context.Context, cfg SSHConfig, command string) (string, error) {
	if cfg.Host == "" || cfg.User == "" {
		return "", fmt.Errorf("SSH is not configured for this network")
	}
	if cfg.Port == 0 {
		cfg.Port = 22
	}

	var auth []ssh.AuthMethod
	if key := strings.TrimSpace(cfg.Key); key != "" {
		signer, err := ssh.ParsePrivateKey([]byte(key))
		if err != nil {
			return "", fmt.Errorf("the SSH key could not be parsed: %w", err)
		}
		auth = append(auth, ssh.PublicKeys(signer))
	}
	if cfg.Password != "" {
		auth = append(auth, ssh.Password(cfg.Password))
	}
	if len(auth) == 0 {
		return "", fmt.Errorf("no SSH password or key is stored for this network")
	}

	dialer := net.Dialer{Timeout: 8 * time.Second}
	tcp, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(cfg.Host, fmt.Sprint(cfg.Port)))
	if err != nil {
		return "", fmt.Errorf("could not reach %s: %w", cfg.Host, err)
	}
	clientConfig := &ssh.ClientConfig{
		User: cfg.User,
		Auth: auth,
		// Homelab reality: host keys are unpinned, same policy as the
		// terminal bridges. The credential only flows TO the host.
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec
		Timeout:         8 * time.Second,
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(tcp, cfg.Host, clientConfig)
	if err != nil {
		_ = tcp.Close()
		return "", fmt.Errorf("SSH login failed: %w", err)
	}
	client := ssh.NewClient(sshConn, chans, reqs)
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("opening an SSH session: %w", err)
	}
	defer session.Close()

	var out bytes.Buffer
	session.Stdout = &out
	session.Stderr = &out

	done := make(chan error, 1)
	go func() { done <- session.Run(command) }()
	select {
	case err := <-done:
		if err != nil {
			return out.String(), fmt.Errorf("command failed: %w", err)
		}
		return out.String(), nil
	case <-ctx.Done():
		_ = session.Signal(ssh.SIGKILL)
		return out.String(), fmt.Errorf("command timed out")
	}
}
