// Package pihole implements the Pi-hole DNS integration, supporting both the
// v6 REST API and the legacy v5 api.php interface behind one driver. The
// version is detected at connect time; capabilities differ accordingly.
package pihole

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"homehub/internal/integration"
	"homehub/internal/integrations/dns"
	"homehub/internal/metrics"
)

const TypeID = "pihole"

type Driver struct{}

func (Driver) TypeID() string      { return TypeID }
func (Driver) DisplayName() string { return "Pi-hole" }
func (Driver) Description() string {
	return "Network-wide ad blocking: statistics, query log, allow/deny lists, clients, and timed disable. Supports Pi-hole v5 and v6."
}
func (Driver) SecretNames() []string { return []string{"password"} }

func baseURL(config map[string]any) (string, *http.Client, error) {
	host, _ := config["host"].(string)
	host = strings.TrimRight(strings.TrimSpace(host), "/")
	if host == "" {
		return "", nil, errors.New("host is required, e.g. pihole.lan or http://192.168.1.5:8080")
	}
	if !strings.Contains(host, "://") {
		host = "http://" + host
	}
	u, err := url.Parse(host)
	if err != nil || u.Host == "" {
		return "", nil, fmt.Errorf("host %q is not a valid address", host)
	}
	insecure, _ := config["insecureTls"].(bool)
	client := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: insecure},
		},
	}
	return u.Scheme + "://" + u.Host + u.Path, client, nil
}

// detect returns a ready provider for whichever API generation answers.
func detect(ctx context.Context, base string, httpClient *http.Client, secret string) (dns.Provider, string, error) {
	// v6 exposes /api/auth; probe it first.
	v6 := &v6Client{base: base, password: secret, http: httpClient}
	if err := v6.authenticate(ctx); err == nil {
		version, _ := v6.Version(ctx)
		return v6, version, nil
	} else if strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "rejected") {
		// The endpoint exists but the password is wrong — that IS v6.
		return nil, "", err
	}

	v5 := &v5Client{base: base, token: secret, http: httpClient}
	if _, err := v5.Blocking(ctx); err == nil {
		version, _ := v5.Version(ctx)
		return v5, version, nil
	} else {
		return nil, "", fmt.Errorf("no Pi-hole API found at this address (tried v6 /api and v5 /admin/api.php): %v", err)
	}
}

func (Driver) Test(ctx context.Context, config map[string]any, secrets map[string]string) integration.TestResult {
	base, httpClient, err := baseURL(config)
	if err != nil {
		return integration.TestResult{OK: false, Message: err.Error()}
	}
	if secrets["password"] == "" {
		return integration.TestResult{OK: false, Message: "the Pi-hole password (v6 app password, or v5 API token) is required"}
	}
	provider, version, err := detect(ctx, base, httpClient, secrets["password"])
	if err != nil {
		return integration.TestResult{OK: false, Message: friendlyError(err)}
	}
	stats, err := provider.Stats(ctx)
	if err != nil {
		return integration.TestResult{OK: false, Message: friendlyError(err)}
	}
	generation := "v6 API"
	if _, isV5 := provider.(*v5Client); isV5 {
		generation = "v5 API (group and client management need v6)"
	}
	return integration.TestResult{
		OK: true,
		Message: fmt.Sprintf("Connected to Pi-hole %s (%s) — %d queries today, %.1f%% blocked, %d domains on lists",
			version, generation, stats.QueriesToday, stats.BlockedPct, stats.DomainsOnLists),
		Detail: map[string]any{"version": version, "generation": generation},
	}
}

func friendlyError(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "connection refused"):
		return "connection refused — check the address and port of the Pi-hole web interface"
	case strings.Contains(msg, "no such host"):
		return "hostname not found — check the address"
	case strings.Contains(msg, "x509") || strings.Contains(msg, "certificate"):
		return "TLS certificate not trusted — enable \"skip TLS verification\" for self-signed Pi-hole setups (" + msg + ")"
	case strings.Contains(msg, "context deadline exceeded") || strings.Contains(msg, "Client.Timeout"):
		return "connection timed out — check the address and any firewalls"
	}
	return msg
}

func (d Driver) Open(ctx context.Context, inst integration.Instance, deps integration.Deps) (integration.Connection, error) {
	base, httpClient, err := baseURL(inst.Config)
	if err != nil {
		return nil, err
	}
	secret, err := deps.Secret(ctx, "password")
	if err != nil {
		return nil, fmt.Errorf("stored password unreadable: %w", err)
	}
	provider, _, err := detect(ctx, base, httpClient, secret)
	if err != nil {
		return nil, errors.New(friendlyError(err))
	}
	return &Conn{Provider: provider, inst: inst}, nil
}

// Conn wraps whichever generation's client answered.
type Conn struct {
	dns.Provider
	inst integration.Instance

	mu          sync.Mutex
	lastTotal   int64
	lastBlocked int64
	lastSample  time.Time
}

func (c *Conn) Close() {}

func (c *Conn) Health(ctx context.Context) integration.Health {
	if _, err := c.Blocking(ctx); err != nil {
		return integration.Health{Status: integration.StatusUnreachable, Message: friendlyError(err)}
	}
	return integration.Health{Status: integration.StatusConnected}
}

func (c *Conn) Resources(ctx context.Context) ([]integration.Resource, error) {
	return []integration.Resource{{Path: c.inst.ResourcePath(), Kind: "instance", Name: c.inst.Name}}, nil
}

// Collect derives query/blocked rates from successive counter readings and
// samples the blocked percentage for trend graphs.
func (c *Conn) Collect(ctx context.Context) ([]metrics.Sample, error) {
	stats, err := c.Stats(ctx)
	if err != nil {
		return nil, err
	}
	return DNSRateSamples(&c.mu, &c.lastTotal, &c.lastBlocked, &c.lastSample,
		c.inst.ResourcePath(), stats)
}

// DNSRateSamples is shared with the AdGuard driver via a tiny internal
// contract: convert absolute daily counters into per-minute rates.
func DNSRateSamples(mu *sync.Mutex, lastTotal, lastBlocked *int64, lastSample *time.Time, resource string, stats *dns.Stats) ([]metrics.Sample, error) {
	now := time.Now()
	samples := []metrics.Sample{
		{Resource: resource, Metric: "blocked_pct", Unit: "fraction", TS: now, Value: stats.BlockedPct / 100},
	}
	mu.Lock()
	defer mu.Unlock()
	if !lastSample.IsZero() {
		elapsed := now.Sub(*lastSample).Minutes()
		totalDelta := float64(stats.QueriesToday - *lastTotal)
		blockedDelta := float64(stats.BlockedToday - *lastBlocked)
		// Counters reset at midnight (or on restart); skip negative deltas.
		if elapsed > 0 && totalDelta >= 0 && blockedDelta >= 0 {
			samples = append(samples,
				metrics.Sample{Resource: resource, Metric: "queries_per_min", Unit: "per_min", TS: now, Value: totalDelta / elapsed},
				metrics.Sample{Resource: resource, Metric: "blocked_per_min", Unit: "per_min", TS: now, Value: blockedDelta / elapsed},
			)
		}
	}
	*lastTotal = stats.QueriesToday
	*lastBlocked = stats.BlockedToday
	*lastSample = now
	return samples, nil
}

// Overview delegates to the underlying API client when it supports the rich
// dashboard data (both v5 and v6 do, with different depth).
func (c *Conn) Overview(ctx context.Context) (*dns.Overview, error) {
	if op, ok := c.Provider.(dns.OverviewProvider); ok {
		return op.Overview(ctx)
	}
	return nil, dns.ErrUnsupported
}
