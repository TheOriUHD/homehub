package term

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/coder/websocket"
	"golang.org/x/crypto/ssh"

	"homehub/internal/sshpin"
)

// SSHTarget describes an SSH endpoint with its credential material.
type SSHTarget struct {
	Host     string
	Port     int
	Username string
	AuthType string // "password" | "key"
	Secret   string // the password, or the PEM private key
}

// OpenSSH returns an OpenFunc that bridges the WebSocket to an interactive
// SSH shell with a PTY.
func OpenSSH(target SSHTarget) OpenFunc {
	return func(ctx context.Context, conn *websocket.Conn) error {
		var auth ssh.AuthMethod
		switch target.AuthType {
		case "key":
			signer, err := ssh.ParsePrivateKey([]byte(target.Secret))
			if err != nil {
				return fmt.Errorf("private key not usable: %w", err)
			}
			auth = ssh.PublicKeys(signer)
		default:
			auth = ssh.Password(target.Secret)
		}

		addr := net.JoinHostPort(target.Host, fmt.Sprint(target.Port))
		// Trust-on-first-use host-key pinning: the first connection records the
		// guest's key; a later mismatch (rebuild or LAN MITM) is refused. A
		// genuine reinstall clears the pin.
		pin := sshpin.For(ctx, target.Host, target.Port)
		client, err := ssh.Dial("tcp", addr, &ssh.ClientConfig{
			User:            target.Username,
			Auth:            []ssh.AuthMethod{auth},
			HostKeyCallback: pin.Verify,
			Timeout:         10 * time.Second,
		})
		if err != nil {
			return fmt.Errorf("SSH connection to %s failed: %w", addr, err)
		}
		pin.Commit(ctx)
		defer client.Close()

		session, err := client.NewSession()
		if err != nil {
			return fmt.Errorf("open SSH session: %w", err)
		}
		defer session.Close()

		stdin, err := session.StdinPipe()
		if err != nil {
			return err
		}
		stdout, err := session.StdoutPipe()
		if err != nil {
			return err
		}
		session.Stderr = writerFunc(func(p []byte) (int, error) {
			wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			if err := conn.Write(wctx, websocket.MessageBinary, p); err != nil {
				return 0, err
			}
			return len(p), nil
		})

		if err := session.RequestPty("xterm-256color", 24, 80, ssh.TerminalModes{}); err != nil {
			return fmt.Errorf("request PTY: %w", err)
		}
		if err := session.Shell(); err != nil {
			return fmt.Errorf("start shell: %w", err)
		}

		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		// Keep the browser leg alive through reverse-proxy idle timeouts.
		KeepAlive(ctx, 30*time.Second, conn)

		// upstream → client
		go func() {
			defer cancel()
			buf := make([]byte, 32*1024)
			for {
				n, rerr := stdout.Read(buf)
				if n > 0 {
					wctx, wcancel := context.WithTimeout(ctx, 10*time.Second)
					werr := conn.Write(wctx, websocket.MessageBinary, buf[:n])
					wcancel()
					if werr != nil {
						return
					}
				}
				if rerr != nil {
					return
				}
			}
		}()

		// client → upstream
		go func() {
			defer cancel()
			for {
				kind, data, rerr := conn.Read(ctx)
				if rerr != nil {
					return
				}
				switch kind {
				case websocket.MessageBinary:
					if _, werr := stdin.Write(data); werr != nil {
						return
					}
				case websocket.MessageText:
					var frame ControlFrame
					if json.Unmarshal(data, &frame) == nil && frame.Type == "resize" {
						_ = session.WindowChange(frame.Rows, frame.Cols)
					}
				}
			}
		}()

		waitErr := make(chan error, 1)
		go func() { waitErr <- session.Wait() }()

		select {
		case <-ctx.Done():
			return nil
		case err := <-waitErr:
			var exitErr *ssh.ExitError
			if err != nil && !errors.As(err, &exitErr) && !errors.Is(err, io.EOF) {
				return err
			}
			SendControl(ctx, conn, ControlFrame{Type: "exit", Message: "session ended"})
			return nil
		}
	}
}

type writerFunc func(p []byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

// dialSSH opens a client to the target with the same auth and host-key
// pinning the interactive bridge uses.
func dialSSH(ctx context.Context, target SSHTarget) (*ssh.Client, error) {
	var auth ssh.AuthMethod
	switch target.AuthType {
	case "key":
		signer, err := ssh.ParsePrivateKey([]byte(target.Secret))
		if err != nil {
			return nil, fmt.Errorf("private key not usable: %w", err)
		}
		auth = ssh.PublicKeys(signer)
	default:
		auth = ssh.Password(target.Secret)
	}
	addr := net.JoinHostPort(target.Host, fmt.Sprint(target.Port))
	pin := sshpin.For(ctx, target.Host, target.Port)
	client, err := ssh.Dial("tcp", addr, &ssh.ClientConfig{
		User:            target.Username,
		Auth:            []ssh.AuthMethod{auth},
		HostKeyCallback: pin.Verify,
		Timeout:         10 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("SSH connection to %s failed: %w", addr, err)
	}
	pin.Commit(ctx)
	return client, nil
}

// TestSSH proves the credentials work, returning the remote's identity.
func TestSSH(ctx context.Context, target SSHTarget) (string, error) {
	client, err := dialSSH(ctx, target)
	if err != nil {
		return "", err
	}
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("open SSH session: %w", err)
	}
	defer session.Close()
	out, err := session.Output("uname -sr 2>/dev/null; hostname 2>/dev/null")
	if err != nil {
		// Authentication already succeeded — a shell-less account still counts.
		return "connected", nil
	}
	return strings.TrimSpace(strings.ReplaceAll(string(out), "\n", " · ")), nil
}

// RunSSH executes one non-interactive command and returns its combined
// output plus the exit status. Output is capped so a runaway command can't
// blow up memory or a chat transcript.
func RunSSH(ctx context.Context, target SSHTarget, command string, maxBytes int) (string, int, error) {
	client, err := dialSSH(ctx, target)
	if err != nil {
		return "", 0, err
	}
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		return "", 0, fmt.Errorf("open SSH session: %w", err)
	}
	defer session.Close()

	var buf strings.Builder
	session.Stdout = &buf
	session.Stderr = &buf
	done := make(chan error, 1)
	go func() { done <- session.Run(command) }()

	exit := 0
	select {
	case <-ctx.Done():
		_ = session.Signal(ssh.SIGKILL)
		return truncate(buf.String(), maxBytes), 0, ctx.Err()
	case rerr := <-done:
		if rerr != nil {
			var ee *ssh.ExitError
			if errors.As(rerr, &ee) {
				exit = ee.ExitStatus()
			} else {
				return truncate(buf.String(), maxBytes), 0, rerr
			}
		}
	}
	return truncate(buf.String(), maxBytes), exit, nil
}

func truncate(s string, max int) string {
	if max > 0 && len(s) > max {
		return s[:max] + "\n… output truncated"
	}
	return s
}

// ---- key provisioning ---------------------------------------------------------

// KeyPair is a freshly minted SSH identity.
type KeyPair struct {
	PrivatePEM string
	PublicKey  string // authorized_keys line
}

// GenerateKeyPair creates an ed25519 SSH keypair in memory.
func GenerateKeyPair(comment string) (*KeyPair, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	block, err := ssh.MarshalPrivateKey(priv, comment)
	if err != nil {
		return nil, err
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return nil, err
	}
	return &KeyPair{
		PrivatePEM: string(pem.EncodeToMemory(block)),
		PublicKey:  strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub))),
	}, nil
}

// InstallAuthorizedKey appends a public key to the target user's
// authorized_keys over an existing (password) session, then proves the key
// actually works by reconnecting with it.
//
// It APPENDS and de-duplicates — never truncates — so an existing key
// (including one another tool installed) is left untouched.
func InstallAuthorizedKey(ctx context.Context, passwordTarget SSHTarget, kp *KeyPair) error {
	client, err := dialSSH(ctx, passwordTarget)
	if err != nil {
		return err
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("open SSH session: %w", err)
	}
	// Single idempotent shell step: create ~/.ssh with the permissions sshd
	// insists on, then append the key only when it isn't already present.
	quoted := shellQuote(kp.PublicKey)
	script := "set -e; mkdir -p ~/.ssh; chmod 700 ~/.ssh; touch ~/.ssh/authorized_keys; " +
		"chmod 600 ~/.ssh/authorized_keys; " +
		"grep -qxF " + quoted + " ~/.ssh/authorized_keys || printf '%s\\n' " + quoted + " >> ~/.ssh/authorized_keys"
	out, runErr := session.CombinedOutput(script)
	session.Close()
	if runErr != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = runErr.Error()
		}
		return fmt.Errorf("installing the key failed: %s", msg)
	}

	// Prove it: authenticate with the new key before we store anything.
	keyTarget := passwordTarget
	keyTarget.AuthType = "key"
	keyTarget.Secret = kp.PrivatePEM
	vctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if _, verr := TestSSH(vctx, keyTarget); verr != nil {
		return fmt.Errorf("the key was installed but did not authenticate — the server may forbid key auth for this user: %w", verr)
	}
	return nil
}

// shellQuote wraps a value in single quotes for safe shell interpolation.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
