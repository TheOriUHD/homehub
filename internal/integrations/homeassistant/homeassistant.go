// Package homeassistant implements the Home Assistant integration over the
// native WebSocket API: live entity states pushed on change, service calls,
// area/device/entity registries, and numeric sensor history.
package homeassistant

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"homehub/internal/integration"
)

const TypeID = "homeassistant"

type Driver struct{}

func (Driver) TypeID() string      { return TypeID }
func (Driver) DisplayName() string { return "Home Assistant" }
func (Driver) Description() string {
	return "Smart home control through Home Assistant: live entity states, lights, climate, covers, media, scenes, automations, and sensor history."
}
func (Driver) SecretNames() []string { return []string{"token"} }

// baseFromConfig normalizes the host config to an http(s) origin.
func baseFromConfig(config map[string]any) (string, bool, error) {
	host, _ := config["host"].(string)
	host = strings.TrimRight(strings.TrimSpace(host), "/")
	if host == "" {
		return "", false, errors.New("host is required, e.g. homeassistant.lan:8123 or http://192.168.1.10:8123")
	}
	if !strings.Contains(host, "://") {
		host = "http://" + host
	}
	u, err := url.Parse(host)
	if err != nil || u.Host == "" {
		return "", false, fmt.Errorf("host %q is not a valid address", host)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", false, fmt.Errorf("unsupported scheme %q — use http or https", u.Scheme)
	}
	insecure, _ := config["insecureTls"].(bool)
	return u.Scheme + "://" + u.Host + u.Path, insecure, nil
}

func friendlyError(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "connection refused"):
		return "connection refused — check the address and port (Home Assistant serves on 8123 by default)"
	case strings.Contains(msg, "no such host"):
		return "hostname not found — check the address"
	case strings.Contains(msg, "x509") || strings.Contains(msg, "certificate"):
		return "TLS certificate not trusted — enable \"skip TLS verification\" for self-signed setups (" + msg + ")"
	case strings.Contains(msg, "context deadline exceeded") || strings.Contains(msg, "Client.Timeout"):
		return "connection timed out — check the address and any firewalls"
	case strings.Contains(msg, "bad handshake") || strings.Contains(msg, "expected handshake response"):
		return "the address answered but not with a WebSocket — check that it points at Home Assistant itself, not a proxy page"
	case strings.Contains(msg, "close frame") || strings.Contains(msg, "EOF") || strings.Contains(msg, "connection reset"):
		return "the connection dropped — Home Assistant went offline or restarted"
	}
	return msg
}

func (Driver) Test(ctx context.Context, config map[string]any, secrets map[string]string) integration.TestResult {
	base, insecure, err := baseFromConfig(config)
	if err != nil {
		return integration.TestResult{OK: false, Message: err.Error()}
	}
	if secrets["token"] == "" {
		return integration.TestResult{OK: false, Message: "a long-lived access token is required — create one under your Home Assistant profile's Security tab"}
	}
	sock, err := dialSocket(ctx, base, insecure, secrets["token"])
	if err != nil {
		return integration.TestResult{OK: false, Message: friendlyError(err)}
	}
	defer sock.close()

	var cfg haConfig
	if err := sock.callInto(ctx, map[string]any{"type": "get_config"}, &cfg); err != nil {
		return integration.TestResult{OK: false, Message: friendlyError(err)}
	}
	var states []rawState
	if err := sock.callInto(ctx, map[string]any{"type": "get_states"}, &states); err != nil {
		return integration.TestResult{OK: false, Message: friendlyError(err)}
	}
	name := cfg.LocationName
	if name == "" {
		name = "Home Assistant"
	}
	return integration.TestResult{
		OK: true,
		Message: fmt.Sprintf("Connected to %s — Home Assistant %s, %d entities",
			name, sock.haVersion, len(states)),
		Detail: map[string]any{"version": sock.haVersion, "entities": len(states)},
	}
}

func (d Driver) Open(ctx context.Context, inst integration.Instance, deps integration.Deps) (integration.Connection, error) {
	base, insecure, err := baseFromConfig(inst.Config)
	if err != nil {
		return nil, err
	}
	token, err := deps.Secret(ctx, "token")
	if err != nil {
		return nil, fmt.Errorf("reading the access token: %w", err)
	}
	sock, err := dialSocket(ctx, base, insecure, token)
	if err != nil {
		return nil, errors.New(friendlyError(err))
	}
	conn := &Conn{inst: inst, deps: deps, sock: sock}
	if err := conn.bootstrap(ctx); err != nil {
		sock.close()
		return nil, errors.New(friendlyError(err))
	}
	return conn, nil
}
