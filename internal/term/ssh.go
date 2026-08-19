package term

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/coder/websocket"
	"golang.org/x/crypto/ssh"

	"github.com/TheOriUHD/homehub/internal/sshpin"
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
