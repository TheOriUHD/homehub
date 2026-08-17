package pvesim

import (
	crand "crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
)

// consoleTicketTTL matches PVE's short-lived VNC ticket validity.
const consoleTicketTTL = 60 * time.Second

// fakeCertPEM is returned by vncproxy; clients only pass it through.
const fakeCertPEM = "-----BEGIN CERTIFICATE-----\nMIIBpvesim0FakeCertNotARealKeyAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\n-----END CERTIFICATE-----\n"

func newTicket(prefix string) string {
	b := make([]byte, 24)
	_, _ = crand.Read(b)
	return fmt.Sprintf("%s:%d::%s", prefix, time.Now().Unix(), base64.RawStdEncoding.EncodeToString(b))
}

func (s *Server) issueTicket(w http.ResponseWriter, r *http.Request, prefix string) (string, bool) {
	kind, nodeName, vmid := chi.URLParam(r, "kind"), chi.URLParam(r, "node"), pathVmid(r)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.findGuest(kind, nodeName, vmid); err != nil {
		writeErr(w, http.StatusInternalServerError, "%v", err)
		return "", false
	}
	tk := newTicket(prefix)
	s.tickets[tk] = &consoleTicket{user: "root@pam", issued: time.Now()}
	return tk, true
}

func (s *Server) postVNCProxy(w http.ResponseWriter, r *http.Request) {
	tk, ok := s.issueTicket(w, r, "PVEVNC")
	if !ok {
		return
	}
	writeData(w, j{"ticket": tk, "port": "5900", "user": "root@pam", "cert": fakeCertPEM})
}

func (s *Server) postTermProxy(w http.ResponseWriter, r *http.Request) {
	tk, ok := s.issueTicket(w, r, "PVETERM")
	if !ok {
		return
	}
	writeData(w, j{"ticket": tk, "port": "5900", "user": "root@pam"})
}

// getVNCWebsocket upgrades to a WebSocket after validating a previously issued
// single-use ticket. Handshake: the client's first message must be
// "<user>:<ticket>\n", answered with a text "OK"; afterwards every message is
// echoed back verbatim so console bridges can be tested end to end.
func (s *Server) getVNCWebsocket(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	tkt := q.Get("vncticket")
	if q.Get("port") == "" || tkt == "" {
		writeErr(w, http.StatusUnauthorized, "missing port or vncticket")
		return
	}
	s.mu.Lock()
	ct, ok := s.tickets[tkt]
	valid := ok && !ct.used && time.Since(ct.issued) <= consoleTicketTTL
	if valid {
		ct.used = true
	}
	s.mu.Unlock()
	if !valid {
		writeErr(w, http.StatusUnauthorized, "invalid or expired vncticket")
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return // Accept already replied
	}
	defer conn.CloseNow()

	ctx := r.Context()
	// Real PVE semantics: only termproxy sessions speak the
	// "<user>:<ticket>\n" → "OK" preamble; VNC sessions go straight to the
	// (here: echoed) byte stream.
	if strings.HasPrefix(tkt, "PVETERM") {
		_, first, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if string(first) != ct.user+":"+tkt+"\n" {
			conn.Close(websocket.StatusPolicyViolation, "bad handshake")
			return
		}
		if err := conn.Write(ctx, websocket.MessageText, []byte("OK")); err != nil {
			return
		}
	}
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if err := conn.Write(ctx, typ, data); err != nil {
			return
		}
	}
}
