// Package caddysim is an in-memory caddy-reader helper used to verify the
// Caddy driver without a real proxy host. It mirrors the helper's documented
// HTTP surface exactly (the caddy-reader protocol): unauthenticated
// /health, the six-digit pairing exchange with expiry / attempt-burning /
// single use, Bearer-token routes with both /x and /x/ registered, the
// {path,text,sha256,writable} Caddyfile envelope, PUT with optimistic
// sha256 concurrency (409 on drift, 400 on a config Caddy would refuse,
// 403 in read-only mode), and stated 404/405 responses elsewhere — so path
// or auth bugs in the driver fail here the way they would in production.
package caddysim

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/TheOriUHD/homehub/internal/caddyfile"
)

const Version = "sim-1.0.0"

// DefaultCaddyfile is the file the sim starts with: a global block, two
// editable sites, an advanced site, and comments worth preserving.
const DefaultCaddyfile = `{
	email admin@example.com
}

# --- sites managed here ---
app.example.com {
	tls {
		dns cloudflare {env.CLOUDFLARE_API_TOKEN}
	}

	encode gzip
	reverse_proxy 10.0.0.5:8443
}

grafana.example.com {
	reverse_proxy 10.0.0.6:3000
}

# static assets
files.example.com {
	root * /srv/files
	file_server browse
}
`

type pairing struct {
	code     string
	token    string
	expires  time.Time
	attempts int
}

// Sim is one fake helper instance.
type Sim struct {
	mu        sync.Mutex
	token     string
	pairing   *pairing
	caddyfile string
	writable  bool
	caddyDown bool
}

func New() *Sim {
	return &Sim{caddyfile: DefaultCaddyfile, writable: true}
}

// ---- test/bench controls ----------------------------------------------------

// OpenPairing opens a fresh pairing window and returns the code.
func (s *Sim) OpenPairing() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	code := randomDigits(6)
	token := randomHex(32)
	s.pairing = &pairing{code: code, token: token, expires: time.Now().Add(15 * time.Minute)}
	return code
}

// ExpirePairing force-expires the current window (for tests).
func (s *Sim) ExpirePairing() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pairing != nil {
		s.pairing.expires = time.Now().Add(-time.Minute)
	}
}

// SetWritable flips write mode, like restarting the helper with --writable.
func (s *Sim) SetWritable(writable bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writable = writable
}

// SetCaddyDown simulates Caddy's admin API being unreachable.
func (s *Sim) SetCaddyDown(down bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.caddyDown = down
}

// SetCaddyfile replaces the stored file (for refusal-path tests).
func (s *Sim) SetCaddyfile(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.caddyfile = text
}

// Caddyfile returns the stored file text.
func (s *Sim) Caddyfile() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.caddyfile
}

// ---- HTTP -------------------------------------------------------------------

func (s *Sim) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSuffix(r.URL.Path, "/")
	if path == "" {
		path = "/"
	}
	switch path {
	case "/health":
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			s.methodNotAllowed(w)
			return
		}
		s.mu.Lock()
		down := s.caddyDown
		s.mu.Unlock()
		if down {
			s.reply(w, http.StatusServiceUnavailable, map[string]any{
				"status": "degraded", "caddy": "unreachable", "version": Version,
			})
			return
		}
		s.reply(w, http.StatusOK, map[string]any{
			"status": "ok", "caddy": "ok", "version": Version,
		})
	case "/pair":
		if r.Method != http.MethodPost {
			s.methodNotAllowed(w)
			return
		}
		s.handlePair(w, r)
	case "/config":
		if !s.authed(r) {
			s.reply(w, http.StatusUnauthorized, map[string]any{"detail": "missing or wrong token"})
			return
		}
		if r.Method != http.MethodGet {
			s.methodNotAllowed(w)
			return
		}
		s.handleConfig(w)
	case "/caddyfile":
		if !s.authed(r) {
			s.reply(w, http.StatusUnauthorized, map[string]any{"detail": "missing or wrong token"})
			return
		}
		switch r.Method {
		case http.MethodGet:
			s.handleGetCaddyfile(w)
		case http.MethodPut:
			s.handlePutCaddyfile(w, r)
		default:
			s.methodNotAllowed(w)
		}
	default:
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			http.Error(w, "this helper serves /health, /pair, /config and /caddyfile", http.StatusNotFound)
			return
		}
		s.methodNotAllowed(w)
	}
}

func (s *Sim) methodNotAllowed(w http.ResponseWriter) {
	http.Error(w, "this helper serves GET /health, POST /pair, GET /config, GET and PUT /caddyfile", http.StatusMethodNotAllowed)
}

func (s *Sim) authed(r *http.Request) bool {
	s.mu.Lock()
	token := s.token
	s.mu.Unlock()
	if token == "" {
		return false
	}
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	return subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1
}

func (s *Sim) handlePair(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		s.reply(w, http.StatusBadRequest, map[string]any{"detail": "the body must be JSON with a code field"})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pairing == nil {
		s.reply(w, http.StatusNotFound, map[string]any{"detail": "no pairing window is open — run `caddy-reader pair` on the Caddy host"})
		return
	}
	if time.Now().After(s.pairing.expires) {
		s.pairing = nil
		s.reply(w, http.StatusGone, map[string]any{"detail": "the pairing code expired — generate a fresh one"})
		return
	}
	if subtle.ConstantTimeCompare([]byte(body.Code), []byte(s.pairing.code)) != 1 {
		s.pairing.attempts++
		if s.pairing.attempts >= 5 {
			s.pairing = nil
			s.reply(w, http.StatusTooManyRequests, map[string]any{"detail": "too many wrong codes — the pairing window is closed; generate a fresh code"})
			return
		}
		s.reply(w, http.StatusUnauthorized, map[string]any{
			"detail":            "wrong code",
			"attemptsRemaining": 5 - s.pairing.attempts,
		})
		return
	}
	token := s.pairing.token
	s.token = token
	s.pairing = nil // single use — the window closes on success
	s.reply(w, http.StatusOK, map[string]any{"token": token, "version": Version})
}

// handleConfig serves the PROJECTED running config. The sim derives a
// plausible projection from its Caddyfile's editable entries rather than
// adapting for real; the shape matches the §5.2 allowlist exactly.
func (s *Sim) handleConfig(w http.ResponseWriter) {
	s.mu.Lock()
	text := s.caddyfile
	down := s.caddyDown
	s.mu.Unlock()
	if down {
		s.reply(w, http.StatusBadGateway, map[string]any{"detail": "Caddy's admin API did not answer"})
		return
	}
	s.reply(w, http.StatusOK, map[string]any{
		"config":    projectionFromCaddyfile(text),
		"projected": true,
		"version":   Version,
	})
}

func (s *Sim) handleGetCaddyfile(w http.ResponseWriter) {
	s.mu.Lock()
	text, writable := s.caddyfile, s.writable
	s.mu.Unlock()
	s.reply(w, http.StatusOK, map[string]any{
		"path":     "/etc/caddy/Caddyfile",
		"text":     text,
		"sha256":   hashText(text),
		"writable": writable,
	})
}

func (s *Sim) handlePutCaddyfile(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	writable := s.writable
	s.mu.Unlock()
	if !writable {
		s.reply(w, http.StatusForbidden, map[string]any{
			"detail": "this helper is read-only — restart it with --writable to allow changes",
		})
		return
	}
	var body struct {
		Text   string `json:"text"`
		Sha256 string `json:"sha256"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<20)).Decode(&body); err != nil {
		s.reply(w, http.StatusBadRequest, map[string]any{"detail": "the body must be JSON with text and sha256 fields"})
		return
	}
	if strings.TrimSpace(body.Text) == "" {
		// An empty Caddyfile is VALID and would take every site offline.
		s.reply(w, http.StatusBadRequest, map[string]any{"detail": "an empty Caddyfile would take every site offline — if that is genuinely wanted, do it by hand"})
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if body.Sha256 != hashText(s.caddyfile) {
		s.reply(w, http.StatusConflict, map[string]any{
			"detail": "The Caddyfile changed since you loaded it — someone edited it by hand, or another tab saved first. Reload and reapply your change so theirs is not lost.",
		})
		return
	}
	// "Caddy validates": brace balance plus a marker directive tests use to
	// force a rejection, reported the way tidyCaddyOutput would.
	if reason := validateLikeCaddy(body.Text); reason != "" {
		s.reply(w, http.StatusBadRequest, map[string]any{"detail": reason})
		return
	}
	s.caddyfile = body.Text
	s.reply(w, http.StatusOK, map[string]any{"sha256": hashText(body.Text)})
}

func validateLikeCaddy(text string) string {
	depth := 0
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasSuffix(trimmed, "{") {
			depth++
		} else if trimmed == "}" {
			depth--
		}
		if strings.Contains(line, "not_a_real_directive") {
			return "Caddyfile:1: unrecognized directive: not_a_real_directive"
		}
	}
	if depth != 0 {
		return "Caddyfile:1: unexpected end of file; braces do not balance"
	}
	return ""
}

// projectionFromCaddyfile builds a §5.2-shaped projection from the sites in
// the file, using the real splitter for block boundaries. Editable entries
// become reverse_proxy routes; raw blocks appear as served domains with a
// generic non-proxy handler, mirroring how a real projection would still
// carry their hosts.
func projectionFromCaddyfile(text string) map[string]any {
	routes := []any{}
	doc := caddyfile.Split(text)
	for _, blk := range doc.Blocks {
		var hosts []any
		var handlers []any
		switch blk.Kind {
		case caddyfile.KindEntry:
			for _, d := range blk.Entry.Domains {
				hosts = append(hosts, d)
			}
			handlers = append(handlers, map[string]any{
				"handler":   "reverse_proxy",
				"upstreams": []any{map[string]any{"dial": dialOf(blk.Entry.Upstream)}},
			})
		case caddyfile.KindRaw:
			for _, a := range blk.Addresses {
				hosts = append(hosts, a)
			}
			handlers = append(handlers, map[string]any{"handler": "file_server"})
		default:
			continue
		}
		if len(hosts) == 0 || len(handlers) == 0 {
			continue
		}
		routes = append(routes, map[string]any{
			"match": []any{map[string]any{"host": hosts}},
			"handle": []any{map[string]any{
				"handler": "subroute",
				"routes":  []any{map[string]any{"handle": handlers}},
			}},
		})
	}
	return map[string]any{
		"apps": map[string]any{
			"http": map[string]any{
				"servers": map[string]any{
					"srv0": map[string]any{"listen": []any{":443"}, "routes": routes},
				},
			},
		},
	}
}

// dialOf renders an upstream the way an adapted config would: host:port,
// scheme stripped.
func dialOf(u caddyfile.Upstream) string {
	host := u.Host
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if u.Port != "" {
		return host + ":" + u.Port
	}
	return host
}

// ---- helpers ---------------------------------------------------------------

func hashText(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func randomDigits(n int) string {
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	out := make([]byte, n)
	for i, b := range raw {
		out[i] = '0' + b%10
	}
	return string(out)
}

func randomHex(bytes int) string {
	raw := make([]byte, bytes)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	return hex.EncodeToString(raw)
}

func (s *Sim) reply(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if payload != nil {
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			fmt.Fprintln(w, "{}")
		}
	}
}
