package term

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	dockerclient "github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"

	"github.com/coder/websocket"
)

// OpenDockerExec bridges the WebSocket to an interactive exec in a container.
// The default command prefers bash and falls back to sh.
func OpenDockerExec(cli *dockerclient.Client, containerID, command string) OpenFunc {
	return func(ctx context.Context, conn *websocket.Conn) error {
		cmd := []string{"/bin/sh", "-c", "command -v bash >/dev/null 2>&1 && exec bash || exec sh"}
		if command != "" {
			cmd = []string{"/bin/sh", "-c", command}
		}
		exec, err := cli.ContainerExecCreate(ctx, containerID, types.ExecConfig{
			AttachStdin:  true,
			AttachStdout: true,
			AttachStderr: true,
			Tty:          true,
			Cmd:          cmd,
		})
		if err != nil {
			return fmt.Errorf("create exec: %w", err)
		}
		attach, err := cli.ContainerExecAttach(ctx, exec.ID, types.ExecStartCheck{Tty: true})
		if err != nil {
			return fmt.Errorf("attach exec: %w", err)
		}
		defer attach.Close()

		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		// Keep the browser leg alive through reverse-proxy idle timeouts.
		KeepAlive(ctx, 30*time.Second, conn)

		// exec output → client
		go func() {
			defer cancel()
			buf := make([]byte, 32*1024)
			for {
				n, rerr := attach.Reader.Read(buf)
				if n > 0 {
					if werr := conn.Write(ctx, websocket.MessageBinary, buf[:n]); werr != nil {
						return
					}
				}
				if rerr != nil {
					return
				}
			}
		}()

		// client input / resize → exec
		for {
			kind, data, rerr := conn.Read(ctx)
			if rerr != nil {
				return nil
			}
			switch kind {
			case websocket.MessageBinary:
				if _, werr := attach.Conn.Write(data); werr != nil {
					return nil
				}
			case websocket.MessageText:
				var frame ControlFrame
				if json.Unmarshal(data, &frame) == nil && frame.Type == "resize" {
					_ = cli.ContainerExecResize(ctx, exec.ID, container.ResizeOptions{
						Height: uint(frame.Rows),
						Width:  uint(frame.Cols),
					})
				}
			}
		}
	}
}

// OpenDockerLogs bridges the WebSocket to a following container log stream —
// read-only; client input is ignored.
func OpenDockerLogs(cli *dockerclient.Client, containerID string, tty bool, tail string) OpenFunc {
	return func(ctx context.Context, conn *websocket.Conn) error {
		reader, err := cli.ContainerLogs(ctx, containerID, container.LogsOptions{
			ShowStdout: true,
			ShowStderr: true,
			Follow:     true,
			Tail:       tail,
		})
		if err != nil {
			return fmt.Errorf("open logs: %w", err)
		}
		defer reader.Close()

		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		// Keep the browser leg alive through reverse-proxy idle timeouts.
		KeepAlive(ctx, 30*time.Second, conn)

		// Drain (and ignore) client frames so pings/close are noticed.
		go func() {
			defer cancel()
			for {
				if _, _, rerr := conn.Read(ctx); rerr != nil {
					return
				}
			}
		}()

		out := wsWriter{ctx: ctx, conn: conn}
		if tty {
			_, err = io.Copy(out, reader)
		} else {
			// Non-TTY streams are multiplexed; interleave both channels and
			// normalize line endings for the terminal renderer.
			_, err = stdcopy.StdCopy(crlfWriter{out}, crlfWriter{out}, reader)
		}
		if err != nil && ctx.Err() == nil {
			return err
		}
		SendControl(ctx, conn, ControlFrame{Type: "exit", Message: "log stream ended"})
		return nil
	}
}

type wsWriter struct {
	ctx  context.Context
	conn *websocket.Conn
}

func (w wsWriter) Write(p []byte) (int, error) {
	if err := w.conn.Write(w.ctx, websocket.MessageBinary, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

// crlfWriter rewrites bare newlines to CRLF so xterm.js renders log lines at
// column zero.
type crlfWriter struct{ inner io.Writer }

func (w crlfWriter) Write(p []byte) (int, error) {
	out := make([]byte, 0, len(p)+16)
	for _, b := range p {
		if b == '\n' {
			out = append(out, '\r', '\n')
		} else {
			out = append(out, b)
		}
	}
	if _, err := w.inner.Write(out); err != nil {
		return 0, err
	}
	return len(p), nil
}
