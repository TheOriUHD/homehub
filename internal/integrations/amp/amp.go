// Package amp implements the CubeCoders AMP integration against the ADS
// (controller) HTTP API: instance inventory with live state and metrics,
// application and instance lifecycle, and per-instance consoles with
// command dispatch.
//
// AMP is licensed software and no licensed panel is available in the build
// environment, so this driver is verified against an in-repo simulator
// (internal/ampsim) that mirrors AMP's documented routing — including the
// ADSModule/Servers/{id}/API proxy nesting and session scoping. It must be
// validated against a real AMP installation before relying on it.
//
// Wire contract (AMP 2.4+): every call is POST {base}/API/{Module}/{Method}
// with a JSON body; auth rides a SESSIONID body field obtained from
// Core/Login. Instance-scoped calls nest through the controller at
// {base}/API/ADSModule/Servers/{instanceID}/API/{Module}/{Method} and hold
// their own session per instance. Expired sessions come back as HTTP 200
// with a {"Title":"Unauthorized Access"} body, not a 401.
package amp

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"homehub/internal/integration"
)

const TypeID = "amp"

type Driver struct{}

func (Driver) TypeID() string      { return TypeID }
func (Driver) DisplayName() string { return "AMP (CubeCoders)" }
func (Driver) Description() string {
	return "CubeCoders AMP game panel: every instance with live state, players, CPU and memory, full console with command dispatch, and application and instance lifecycle control."
}
func (Driver) SecretNames() []string { return []string{"password"} }

// client speaks the AMP API with one session per scope: "" is the
// controller, an instance ID is the ADS-proxied path to that instance.
type client struct {
	base     string
	username string
	password string
	http     *http.Client

	mu       sync.Mutex
	sessions map[string]string // scope -> SESSIONID
}

func newClient(config map[string]any, secrets map[string]string) (*client, error) {
	host, _ := config["host"].(string)
	host = strings.TrimRight(strings.TrimSpace(host), "/")
	if host == "" {
		return nil, errors.New("host is required, e.g. amp.lan:8080 or https://panel.example.com")
	}
	if !strings.Contains(host, "://") {
		host = "http://" + host
	}
	u, err := url.Parse(host)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("host %q is not a valid address", host)
	}
	username, _ := config["username"].(string)
	if strings.TrimSpace(username) == "" {
		return nil, errors.New("a username is required — use a dedicated AMP user with the permissions you want HomeHub to have")
	}
	if secrets["password"] == "" {
		return nil, errors.New("a password is required")
	}
	insecure, _ := config["insecureTls"].(bool)
	return &client{
		base:     u.Scheme + "://" + u.Host + strings.TrimRight(u.Path, "/"),
		username: strings.TrimSpace(username),
		password: secrets["password"],
		http: &http.Client{
			Timeout: 25 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: insecure},
			},
		},
		sessions: map[string]string{},
	}, nil
}

// apiURL builds the endpoint for a scope: the controller directly, or an
// instance through the ADS proxy. The proxy nesting is part of AMP's real
// routing — instance sessions are only valid on the proxied path.
func (c *client) apiURL(scope, api string) string {
	if scope == "" {
		return c.base + "/API/" + api
	}
	return c.base + "/API/ADSModule/Servers/" + url.PathEscape(scope) + "/API/" + api
}

// login opens a fresh session for a scope and stores it.
func (c *client) login(ctx context.Context, scope string) error {
	params := map[string]any{
		"username":   c.username,
		"password":   c.password,
		"token":      "",
		"rememberMe": false,
	}
	var res LoginResult
	if err := c.post(ctx, c.apiURL(scope, "Core/Login"), params, &res); err != nil {
		return err
	}
	if !res.Success || res.SessionID == "" {
		reason := res.ResultReason
		if reason == "" {
			reason = "login failed"
		}
		return fmt.Errorf("AMP rejected the login: %s", reason)
	}
	c.mu.Lock()
	c.sessions[scope] = res.SessionID
	c.mu.Unlock()
	return nil
}

// Call performs one API call in a scope, logging in on demand and retrying
// once when AMP reports the session as expired.
func (c *client) Call(ctx context.Context, scope, api string, params map[string]any, out any) error {
	for attempt := 0; ; attempt++ {
		c.mu.Lock()
		session := c.sessions[scope]
		c.mu.Unlock()
		if session == "" {
			if err := c.login(ctx, scope); err != nil {
				return err
			}
			c.mu.Lock()
			session = c.sessions[scope]
			c.mu.Unlock()
		}

		body := map[string]any{"SESSIONID": session}
		for k, v := range params {
			body[k] = v
		}
		var raw json.RawMessage
		if err := c.post(ctx, c.apiURL(scope, api), body, &raw); err != nil {
			return err
		}
		if isSessionExpired(raw) {
			c.mu.Lock()
			delete(c.sessions, scope)
			c.mu.Unlock()
			if attempt == 0 {
				continue
			}
			return errors.New("AMP rejected the session twice — check the account's permissions")
		}
		if title := errorTitle(raw); title != "" {
			return fmt.Errorf("AMP: %s", title)
		}
		if out != nil && len(raw) > 0 && string(raw) != "null" {
			if err := json.Unmarshal(raw, out); err != nil {
				return fmt.Errorf("%s: decode response: %w", api, err)
			}
		}
		return nil
	}
}

func (c *client) post(ctx context.Context, endpoint string, body, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/javascript")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("AMP request failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// isSessionExpired matches AMP's 200-with-error envelope for a dead session:
// either a bare string or an object whose Title says so.
func isSessionExpired(raw json.RawMessage) bool {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s == "Unauthorized Access"
	}
	return errorTitle(raw) == "Unauthorized Access"
}

// errorTitle extracts AMP's {"Title": "..."} error envelope, empty when the
// response is a normal payload.
func errorTitle(raw json.RawMessage) string {
	var probe struct {
		Title any `json:"Title"`
	}
	if json.Unmarshal(raw, &probe) != nil {
		return ""
	}
	if title, ok := probe.Title.(string); ok {
		switch title {
		case "Unauthorized Access", "Instance Unavailable":
			return title
		}
	}
	return ""
}

func friendlyError(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "connection refused"):
		return "connection refused — check the address and that the AMP panel is reachable"
	case strings.Contains(msg, "no such host"):
		return "hostname not found — check the address"
	case strings.Contains(msg, "x509") || strings.Contains(msg, "certificate"):
		return "TLS certificate not trusted — enable \"skip TLS verification\" for a self-signed panel (" + msg + ")"
	case strings.Contains(msg, "context deadline exceeded") || strings.Contains(msg, "Client.Timeout"):
		return "connection timed out — check the address and any firewalls"
	}
	return msg
}

func (Driver) Test(ctx context.Context, config map[string]any, secrets map[string]string) integration.TestResult {
	c, err := newClient(config, secrets)
	if err != nil {
		return integration.TestResult{OK: false, Message: err.Error()}
	}
	targets, err := fetchTargets(ctx, c)
	if err != nil {
		return integration.TestResult{OK: false, Message: friendlyError(err)}
	}
	instances := 0
	for _, t := range targets {
		instances += len(t.AvailableInstances)
	}
	return integration.TestResult{
		OK:      true,
		Message: fmt.Sprintf("Connected — %d target(s), %d instance(s)", len(targets), instances),
		Detail:  map[string]any{"targets": len(targets), "instances": instances},
	}
}

func (d Driver) Open(ctx context.Context, inst integration.Instance, deps integration.Deps) (integration.Connection, error) {
	password, err := deps.Secret(ctx, "password")
	if err != nil {
		return nil, fmt.Errorf("reading the password: %w", err)
	}
	c, err := newClient(inst.Config, map[string]string{"password": password})
	if err != nil {
		return nil, err
	}
	conn := newConn(inst, c, deps)
	pctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if _, err := conn.Targets(pctx); err != nil {
		return nil, errors.New(friendlyError(err))
	}
	return conn, nil
}

// fetchTargets pulls the raw target list from the controller.
func fetchTargets(ctx context.Context, c *client) ([]Target, error) {
	var targets []Target
	if err := c.Call(ctx, "", "ADSModule/GetInstances",
		map[string]any{"ForceIncludeSelf": true}, &targets); err != nil {
		return nil, err
	}
	return targets, nil
}
