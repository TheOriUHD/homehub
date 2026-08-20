// Package caddy manages a Caddy reverse proxy through a caddy-reader helper
// running on the Caddy host (protocol: references/caddy/caddy.md). Caddy's
// admin API is unauthenticated and must never leave loopback; the helper is
// the narrow, token-authenticated surface HomeHub talks to.
//
// The Caddyfile is the source of truth, never a cache: every operation is a
// full read-modify-write against the live file, with sha256 optimistic
// concurrency so a hand edit on the box is never silently overwritten.
//
// HomeHub implements the CLIENT of the documented helper protocol — it does
// not ship or install the helper binary itself.
package caddy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"homehub/internal/integration"
)

const TypeID = "caddy"

// maxBodyBytes bounds every response read (streamed, not buffered blindly).
const maxBodyBytes = 8 << 20

type Driver struct{}

func (Driver) TypeID() string      { return TypeID }
func (Driver) DisplayName() string { return "Caddy" }
func (Driver) Description() string {
	return "A Caddy reverse proxy, managed through the caddy-reader helper on its host: every site in the Caddyfile listed live, graphical add/edit/remove with Caddy validating each change before anything touches disk, and the running config's served domains."
}

// SecretNames: "token" authenticates the caddy-reader helper; "cfToken" is the
// optional Cloudflare API token when the instance is linked to a zone.
func (Driver) SecretNames() []string { return []string{"token", CFSecretName} }

// client speaks the helper protocol with a Bearer token.
type client struct {
	base  string
	token string
	http  *http.Client
}

func normalizeBaseURL(raw string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(raw), "/")
	if base == "" {
		return "", errors.New("the helper address is required, e.g. 10.0.0.2:9776")
	}
	if !strings.Contains(base, "://") {
		base = "http://" + base
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("%q is not a valid address", raw)
	}
	// The most dangerous available mistake, refused outright: Caddy's own
	// admin API has no authentication and must never be the target.
	if u.Port() == "2019" {
		return "", errors.New("that looks like Caddy's admin API, which has no authentication — point this at the caddy-reader helper (default port 9776), never at the admin API")
	}
	return u.Scheme + "://" + u.Host + strings.TrimRight(u.Path, "/"), nil
}

func newClient(config map[string]any, secrets map[string]string) (*client, error) {
	baseURL, _ := config["baseUrl"].(string)
	base, err := normalizeBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	if secrets["token"] == "" {
		return nil, errors.New("this helper is not paired — pair it with a fresh code")
	}
	return &client{base: base, token: secrets["token"], http: helperHTTPClient(30 * time.Second)}, nil
}

func helperHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		// The helper's address is trusted; anything it redirects to is not.
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// helperError is a non-2xx answer, carrying the helper's detail sentence.
type helperError struct {
	Status int
	Detail string
}

func (e *helperError) Error() string {
	if e.Detail != "" {
		return e.Detail
	}
	// Say which of the two machines is broken.
	switch e.Status {
	case http.StatusUnauthorized:
		return "the helper rejected the token — re-pair it with a fresh code"
	case http.StatusForbidden:
		return "this helper is read-only — restart it with --writable to allow changes"
	case http.StatusNotFound:
		return "the helper does not serve this route — it may be too old for this feature"
	case http.StatusBadGateway, http.StatusServiceUnavailable:
		return "the helper is up but cannot reach Caddy on its host"
	}
	return fmt.Sprintf("the helper answered %d", e.Status)
}

// IsConflict reports the 409 optimistic-concurrency answer.
func IsConflict(err error) bool {
	var he *helperError
	return errors.As(err, &he) && he.Status == http.StatusConflict
}

// IsRejected reports a 400 — Caddy refused the config; nothing was touched.
func IsRejected(err error) bool {
	var he *helperError
	return errors.As(err, &he) && he.Status == http.StatusBadRequest
}

// IsReadOnly reports the 403 read-only answer.
func IsReadOnly(err error) bool {
	var he *helperError
	return errors.As(err, &he) && he.Status == http.StatusForbidden
}

func (c *client) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return friendlyNetErr(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		he := &helperError{Status: resp.StatusCode}
		var detail struct {
			Detail string `json:"detail"`
		}
		if json.Unmarshal(raw, &detail) == nil {
			he.Detail = detail.Detail
		}
		return he
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("the helper's answer could not be read: %w", err)
		}
	}
	return nil
}

func friendlyNetErr(err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "connection refused"):
		return errors.New("connection refused — check the helper's address and that caddy-reader is running on the Caddy host")
	case strings.Contains(msg, "no such host"):
		return errors.New("hostname not found — check the helper's address")
	case strings.Contains(msg, "context deadline exceeded") || strings.Contains(msg, "Client.Timeout"):
		return errors.New("the helper did not answer in time — check the address and any firewalls")
	}
	return err
}

// Pair exchanges a six-digit code for the helper's token. The code is not
// the credential — it buys exactly one exchange, then the window closes.
func Pair(ctx context.Context, baseURL, code string) (string, error) {
	base, err := normalizeBaseURL(baseURL)
	if err != nil {
		return "", err
	}
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) != 6 {
		return "", errors.New("the pairing code is six digits")
	}
	c := &client{base: base, http: helperHTTPClient(10 * time.Second)}
	var res struct {
		Token string `json:"token"`
	}
	if err := c.do(ctx, http.MethodPost, "/pair", map[string]string{"code": code}, &res); err != nil {
		var he *helperError
		if errors.As(err, &he) {
			switch he.Status {
			case http.StatusNotFound:
				if he.Detail == "" {
					return "", errors.New("no pairing window is open — run `caddy-reader pair` on the Caddy host first")
				}
			case http.StatusGone:
				if he.Detail == "" {
					return "", errors.New("the pairing code expired — generate a fresh one on the Caddy host")
				}
			}
		}
		return "", err
	}
	if res.Token == "" {
		return "", errors.New("the helper answered without a token — it may not be caddy-reader")
	}
	return res.Token, nil
}

func (Driver) Test(ctx context.Context, config map[string]any, secrets map[string]string) integration.TestResult {
	c, err := newClient(config, secrets)
	if err != nil {
		return integration.TestResult{OK: false, Message: err.Error()}
	}
	state, err := fetchCaddyfile(ctx, c)
	if err != nil {
		return integration.TestResult{OK: false, Message: err.Error()}
	}
	mode := "read-only"
	if state.Writable {
		mode = "writable"
	}
	return integration.TestResult{
		OK:      true,
		Message: fmt.Sprintf("Connected — %s (%s helper)", state.Path, mode),
		Detail:  map[string]any{"path": state.Path, "writable": state.Writable},
	}
}

func (d Driver) Open(ctx context.Context, inst integration.Instance, deps integration.Deps) (integration.Connection, error) {
	token, err := deps.Secret(ctx, "token")
	if err != nil {
		return nil, fmt.Errorf("reading the helper token: %w", err)
	}
	c, err := newClient(inst.Config, map[string]string{"token": token})
	if err != nil {
		return nil, err
	}
	conn := &Conn{inst: inst, client: c, deps: deps}
	pctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if _, err := fetchCaddyfile(pctx, c); err != nil {
		return nil, err
	}
	return conn, nil
}
