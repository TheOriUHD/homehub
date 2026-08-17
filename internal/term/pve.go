package term

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/coder/websocket"

	"homehub/internal/pve"
)

// OpenPVEVNC bridges a client WebSocket to a Proxmox VNC websocket. The
// stream is raw RFB in both directions; the frontend's noVNC answers VNC
// authentication with the proxy ticket returned alongside the session.
func OpenPVEVNC(client *pve.Client, node, kind string, vmid int, ticket pve.VNCProxyTicket) OpenFunc {
	return func(ctx context.Context, conn *websocket.Conn) error {
		upstream, err := client.DialVNCWebsocket(ctx, node, kind, vmid, int64(ticket.Port), ticket.Ticket)
		if err != nil {
			return fmt.Errorf("connect to Proxmox console: %w", err)
		}
		defer upstream.Close(websocket.StatusNormalClosure, "")
		pipe(ctx, conn, upstream)
		return nil
	}
}

// OpenPVESerial bridges a client WebSocket to a Proxmox termproxy websocket,
// translating our terminal protocol to the pve-xtermjs framing: an auth
// preamble, then "0:<len>:<data>" input frames and "1:<cols>:<rows>" resizes,
// with raw output coming back.
func OpenPVESerial(client *pve.Client, node, kind string, vmid int, ticket pve.TermProxyTicket) OpenFunc {
	return func(ctx context.Context, conn *websocket.Conn) error {
		upstream, err := client.DialVNCWebsocket(ctx, node, kind, vmid, int64(ticket.Port), ticket.Ticket)
		if err != nil {
			return fmt.Errorf("connect to Proxmox serial console: %w", err)
		}
		defer upstream.Close(websocket.StatusNormalClosure, "")

		if err := upstream.Write(ctx, websocket.MessageText, []byte(ticket.User+":"+ticket.Ticket+"\n")); err != nil {
			return fmt.Errorf("console authentication failed: %w", err)
		}
		_, first, err := upstream.Read(ctx)
		if err != nil || string(first) != "OK" {
			return fmt.Errorf("Proxmox rejected the console ticket")
		}

		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		// upstream → client: raw terminal output.
		go func() {
			defer cancel()
			for {
				_, data, rerr := upstream.Read(ctx)
				if rerr != nil {
					return
				}
				if len(data) == 0 {
					continue
				}
				if werr := conn.Write(ctx, websocket.MessageBinary, data); werr != nil {
					return
				}
			}
		}()

		// client → upstream: wrap in pve-xtermjs frames.
		for {
			kind, data, rerr := conn.Read(ctx)
			if rerr != nil {
				return nil
			}
			switch kind {
			case websocket.MessageBinary:
				frame := []byte("0:" + strconv.Itoa(len(data)) + ":")
				frame = append(frame, data...)
				if werr := upstream.Write(ctx, websocket.MessageText, frame); werr != nil {
					return nil
				}
			case websocket.MessageText:
				var control ControlFrame
				if err := json.Unmarshal(data, &control); err == nil && control.Type == "resize" {
					frame := fmt.Sprintf("1:%d:%d:", control.Cols, control.Rows)
					if werr := upstream.Write(ctx, websocket.MessageText, []byte(frame)); werr != nil {
						return nil
					}
				}
			}
		}
	}
}

// pipe copies frames verbatim in both directions until either side closes.
func pipe(ctx context.Context, a, b *websocket.Conn) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	copy := func(dst, src *websocket.Conn) {
		defer cancel()
		for {
			kind, data, err := src.Read(ctx)
			if err != nil {
				return
			}
			if err := dst.Write(ctx, kind, data); err != nil {
				return
			}
		}
	}
	go copy(a, b)
	copy(b, a)
}
