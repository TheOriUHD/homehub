package docker

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/TheOriUHD/homehub/internal/sshpin"
)

// sshDialer opens connections to a remote Docker daemon by running
// `docker system dial-stdio` over SSH and treating the process stdio as the
// wire. This is exactly what the docker CLI does for ssh:// hosts, but
// implemented with in-process SSH so credentials come from the vault and no
// ssh binary or agent is needed inside the HomeHub container.
type sshDialer struct {
	addr     string
	username string
	authType string
	secret   string
}

func (d *sshDialer) DialContext(ctx context.Context, _, _ string) (net.Conn, error) {
	var auth ssh.AuthMethod
	if d.authType == "key" {
		signer, err := ssh.ParsePrivateKey([]byte(d.secret))
		if err != nil {
			return nil, fmt.Errorf("SSH private key not usable: %w", err)
		}
		auth = ssh.PublicKeys(signer)
	} else {
		auth = ssh.Password(d.secret)
	}

	dialer := net.Dialer{Timeout: 10 * time.Second}
	raw, err := dialer.DialContext(ctx, "tcp", d.addr)
	if err != nil {
		return nil, err
	}
	// Trust-on-first-use host-key pinning for the remote Docker daemon.
	host, portStr, _ := net.SplitHostPort(d.addr)
	port, _ := strconv.Atoi(portStr)
	pin := sshpin.For(ctx, host, port)
	sshConn, chans, reqs, err := ssh.NewClientConn(raw, d.addr, &ssh.ClientConfig{
		User:            d.username,
		Auth:            []ssh.AuthMethod{auth},
		HostKeyCallback: pin.Verify,
		Timeout:         10 * time.Second,
	})
	if err != nil {
		raw.Close()
		return nil, err
	}
	pin.Commit(ctx)
	client := ssh.NewClient(sshConn, chans, reqs)

	session, err := client.NewSession()
	if err != nil {
		client.Close()
		return nil, err
	}
	stdin, err := session.StdinPipe()
	if err != nil {
		client.Close()
		return nil, err
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		client.Close()
		return nil, err
	}
	if err := session.Start("docker system dial-stdio"); err != nil {
		client.Close()
		return nil, fmt.Errorf("docker system dial-stdio: %w", err)
	}

	return &sshStdioConn{
		stdin:   stdin,
		stdout:  stdout,
		session: session,
		client:  client,
	}, nil
}

// sshStdioConn adapts a remote process's stdio to net.Conn.
type sshStdioConn struct {
	stdin   interface{ Write([]byte) (int, error) }
	stdout  interface{ Read([]byte) (int, error) }
	session *ssh.Session
	client  *ssh.Client
	once    sync.Once
}

func (c *sshStdioConn) Read(p []byte) (int, error)  { return c.stdout.Read(p) }
func (c *sshStdioConn) Write(p []byte) (int, error) { return c.stdin.Write(p) }

func (c *sshStdioConn) Close() error {
	c.once.Do(func() {
		c.session.Close()
		c.client.Close()
	})
	return nil
}

func (c *sshStdioConn) LocalAddr() net.Addr                { return dummyAddr{} }
func (c *sshStdioConn) RemoteAddr() net.Addr               { return dummyAddr{} }
func (c *sshStdioConn) SetDeadline(_ time.Time) error      { return nil }
func (c *sshStdioConn) SetReadDeadline(_ time.Time) error  { return nil }
func (c *sshStdioConn) SetWriteDeadline(_ time.Time) error { return nil }

type dummyAddr struct{}

func (dummyAddr) Network() string { return "ssh-stdio" }
func (dummyAddr) String() string  { return "ssh-stdio" }
