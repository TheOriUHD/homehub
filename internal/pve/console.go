package pve

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/coder/websocket"
)

// VNCProxy allocates a VNC console ticket. Set wantWebsocket to use the
// ticket with DialVNCWebsocket.
func (c *Client) VNCProxy(ctx context.Context, node, kind string, vmid int, wantWebsocket bool) (VNCProxyTicket, error) {
	if err := checkKind(kind); err != nil {
		return VNCProxyTicket{}, err
	}
	var form url.Values
	if wantWebsocket {
		form = url.Values{"websocket": {"1"}}
	}
	var ticket VNCProxyTicket
	err := c.do(ctx, http.MethodPost, guestPath(node, kind, vmid, "vncproxy"), nil, form, &ticket)
	return ticket, err
}

// TermProxy allocates an xterm.js terminal ticket for a guest console.
func (c *Client) TermProxy(ctx context.Context, node, kind string, vmid int) (TermProxyTicket, error) {
	if err := checkKind(kind); err != nil {
		return TermProxyTicket{}, err
	}
	var ticket TermProxyTicket
	err := c.do(ctx, http.MethodPost, guestPath(node, kind, vmid, "termproxy"), nil, nil, &ticket)
	return ticket, err
}

// DialVNCWebsocket opens the guest-scoped vncwebsocket endpoint for a ticket
// from VNCProxy or TermProxy, using the client's auth header and TLS config
// with subprotocol "binary". The ticket is bound to the guest path — the
// node-level /nodes/{node}/vncwebsocket endpoint serves node shells only,
// and real PVE answers guest tickets there with 401 "invalid PVEVNC ticket".
// It returns the raw connection: callers perform the "user:ticket\n" auth
// preamble themselves and must Close it.
func (c *Client) DialVNCWebsocket(ctx context.Context, node, kind string, vmid int, port int64, ticket string) (*websocket.Conn, error) {
	if err := checkKind(kind); err != nil {
		return nil, err
	}
	q := url.Values{
		"port":      {strconv.FormatInt(port, 10)},
		"vncticket": {ticket},
	}
	// websocket.Dial maps http(s) schemes to ws(s) itself.
	wsURL := c.baseURL + "/api2/json" + guestPath(node, kind, vmid, "vncwebsocket") + "?" + q.Encode()
	hdr := http.Header{}
	hdr.Set("Authorization", c.authz)
	conn, resp, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPClient:   c.wsHC,
		HTTPHeader:   hdr,
		Subprotocols: []string{"binary"},
	})
	if err != nil {
		if resp != nil {
			return nil, fmt.Errorf("pve: vnc websocket dial (%s): %w", resp.Status, err)
		}
		return nil, fmt.Errorf("pve: vnc websocket dial: %w", err)
	}
	return conn, nil
}
