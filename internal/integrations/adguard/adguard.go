// Package adguard implements the AdGuard Home DNS integration. User rules
// are parsed into the normalized allow/deny(/regex) list types where the
// syntax maps cleanly; anything else surfaces as a "custom" rule verbatim.
package adguard

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
	"homehub/internal/integrations/dns"
	"homehub/internal/integrations/pihole"
	"homehub/internal/metrics"
)

const TypeID = "adguard"

type Driver struct{}

func (Driver) TypeID() string      { return TypeID }
func (Driver) DisplayName() string { return "AdGuard Home" }
func (Driver) Description() string {
	return "Network-wide ad blocking with AdGuard Home: statistics, query log, custom rules, clients, and protection pause."
}
func (Driver) SecretNames() []string { return []string{"username", "password"} }

type client struct {
	base string
	user string
	pass string
	http *http.Client
}

func newClient(config map[string]any, secrets map[string]string) (*client, error) {
	host, _ := config["host"].(string)
	host = strings.TrimRight(strings.TrimSpace(host), "/")
	if host == "" {
		return nil, errors.New("host is required, e.g. adguard.lan:3000 or http://192.168.1.6")
	}
	if !strings.Contains(host, "://") {
		host = "http://" + host
	}
	u, err := url.Parse(host)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("host %q is not a valid address", host)
	}
	if secrets["username"] == "" || secrets["password"] == "" {
		return nil, errors.New("the AdGuard Home username and password are required")
	}
	insecure, _ := config["insecureTls"].(bool)
	return &client{
		base: u.Scheme + "://" + u.Host + u.Path,
		user: secrets["username"],
		pass: secrets["password"],
		http: &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: insecure},
			},
		},
	}, nil
}

func (c *client) call(ctx context.Context, method, path string, body any, out any) error {
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
	req.SetBasicAuth(c.user, c.pass)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return fmt.Errorf("AdGuard Home rejected the credentials (%d) — check username and password", resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("AdGuard Home request failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func friendlyError(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "connection refused"):
		return "connection refused — check the address and port of the AdGuard Home web interface"
	case strings.Contains(msg, "no such host"):
		return "hostname not found — check the address"
	case strings.Contains(msg, "x509") || strings.Contains(msg, "certificate"):
		return "TLS certificate not trusted — enable \"skip TLS verification\" for self-signed setups (" + msg + ")"
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
	var status struct {
		Version           string `json:"version"`
		ProtectionEnabled bool   `json:"protection_enabled"`
		Running           bool   `json:"running"`
	}
	if err := c.call(ctx, "GET", "/control/status", nil, &status); err != nil {
		return integration.TestResult{OK: false, Message: friendlyError(err)}
	}
	provider := &Conn{client: c}
	stats, err := provider.Stats(ctx)
	if err != nil {
		return integration.TestResult{OK: false, Message: friendlyError(err)}
	}
	return integration.TestResult{
		OK: true,
		Message: fmt.Sprintf("Connected to AdGuard Home %s — %d queries in the current period, %.1f%% blocked",
			status.Version, stats.QueriesToday, stats.BlockedPct),
		Detail: map[string]any{"version": status.Version},
	}
}

func (d Driver) Open(ctx context.Context, inst integration.Instance, deps integration.Deps) (integration.Connection, error) {
	secrets := map[string]string{}
	for _, name := range d.SecretNames() {
		if v, err := deps.Secret(ctx, name); err == nil {
			secrets[name] = v
		}
	}
	c, err := newClient(inst.Config, secrets)
	if err != nil {
		return nil, err
	}
	conn := &Conn{client: c, inst: inst}
	if _, err := conn.Blocking(ctx); err != nil {
		return nil, errors.New(friendlyError(err))
	}
	return conn, nil
}

// Conn implements integration.Connection and dns.Provider.
type Conn struct {
	client *client
	inst   integration.Instance

	mu          sync.Mutex
	lastTotal   int64
	lastBlocked int64
	lastSample  time.Time
}

func (c *Conn) Close() {}

func (c *Conn) Capabilities() dns.Capabilities {
	return dns.Capabilities{ClientCRUD: true, RegexLists: true, CustomRules: true}
}

func (c *Conn) Health(ctx context.Context) integration.Health {
	if _, err := c.Blocking(ctx); err != nil {
		return integration.Health{Status: integration.StatusUnreachable, Message: friendlyError(err)}
	}
	return integration.Health{Status: integration.StatusConnected}
}

func (c *Conn) Resources(ctx context.Context) ([]integration.Resource, error) {
	return []integration.Resource{{Path: c.inst.ResourcePath(), Kind: "instance", Name: c.inst.Name}}, nil
}

func (c *Conn) Collect(ctx context.Context) ([]metrics.Sample, error) {
	stats, err := c.Stats(ctx)
	if err != nil {
		return nil, err
	}
	return pihole.DNSRateSamples(&c.mu, &c.lastTotal, &c.lastBlocked, &c.lastSample,
		c.inst.ResourcePath(), stats)
}

// ---- dns.Provider ----------------------------------------------------------

func (c *Conn) Stats(ctx context.Context) (*dns.Stats, error) {
	var stats struct {
		NumQueries         int64 `json:"num_dns_queries"`
		NumBlocked         int64 `json:"num_blocked_filtering"`
		NumBlockedSafebro  int64 `json:"num_replaced_safebrowsing"`
		NumBlockedParental int64 `json:"num_replaced_parental"`
	}
	if err := c.client.call(ctx, "GET", "/control/stats", nil, &stats); err != nil {
		return nil, err
	}
	var status struct {
		Version           string `json:"version"`
		ProtectionEnabled bool   `json:"protection_enabled"`
		DisabledDuration  int64  `json:"protection_disabled_duration"`
	}
	if err := c.client.call(ctx, "GET", "/control/status", nil, &status); err != nil {
		return nil, err
	}
	var filtering struct {
		Filters []struct {
			Enabled    bool  `json:"enabled"`
			RulesCount int64 `json:"rules_count"`
		} `json:"filters"`
		UserRules []string `json:"user_rules"`
	}
	if err := c.client.call(ctx, "GET", "/control/filtering/status", nil, &filtering); err != nil {
		return nil, err
	}
	var rules int64
	for _, f := range filtering.Filters {
		if f.Enabled {
			rules += f.RulesCount
		}
	}
	rules += int64(len(filtering.UserRules))

	clients, _ := c.Clients(ctx)

	blocked := stats.NumBlocked + stats.NumBlockedSafebro + stats.NumBlockedParental
	pct := 0.0
	if stats.NumQueries > 0 {
		pct = float64(blocked) / float64(stats.NumQueries) * 100
	}
	out := &dns.Stats{
		QueriesToday:   stats.NumQueries,
		BlockedToday:   blocked,
		BlockedPct:     pct,
		ClientsSeen:    int64(len(clients)),
		DomainsOnLists: rules,
		Enabled:        status.ProtectionEnabled,
		Version:        status.Version,
	}
	if status.DisabledDuration > 0 {
		until := time.Now().Add(time.Duration(status.DisabledDuration) * time.Millisecond)
		out.DisabledUntil = &until
	}
	return out, nil
}

func (c *Conn) Blocking(ctx context.Context) (*dns.BlockingState, error) {
	var status struct {
		ProtectionEnabled bool  `json:"protection_enabled"`
		DisabledDuration  int64 `json:"protection_disabled_duration"`
	}
	if err := c.client.call(ctx, "GET", "/control/status", nil, &status); err != nil {
		return nil, err
	}
	state := &dns.BlockingState{Enabled: status.ProtectionEnabled}
	if status.DisabledDuration > 0 {
		until := time.Now().Add(time.Duration(status.DisabledDuration) * time.Millisecond)
		state.Until = &until
	}
	return state, nil
}

func (c *Conn) SetBlocking(ctx context.Context, enabled bool, duration time.Duration) error {
	payload := map[string]any{"enabled": enabled}
	if !enabled && duration > 0 {
		payload["duration"] = duration.Milliseconds()
	}
	return c.client.call(ctx, "POST", "/control/protection", payload, nil)
}

func (c *Conn) Queries(ctx context.Context, filter dns.QueryFilter) ([]dns.QueryEntry, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	params := url.Values{"limit": {fmt.Sprint(limit)}}
	if filter.Search != "" {
		params.Set("search", filter.Search)
	}
	switch filter.Status {
	case dns.StatusBlocked:
		params.Set("response_status", "blocked")
	case dns.StatusAllowed:
		params.Set("response_status", "processed")
	}
	var out struct {
		Data []struct {
			Time     string `json:"time"`
			Client   string `json:"client"`
			Cached   bool   `json:"cached"`
			Upstream string `json:"upstream"`
			Reason   string `json:"reason"`
			Rule     string `json:"rule"`
			Question struct {
				Name string `json:"name"`
				Type string `json:"type"`
			} `json:"question"`
			ClientInfo *struct {
				Name string `json:"name"`
			} `json:"client_info"`
		} `json:"data"`
	}
	if err := c.client.call(ctx, "GET", "/control/querylog?"+params.Encode(), nil, &out); err != nil {
		return nil, err
	}
	entries := make([]dns.QueryEntry, 0, len(out.Data))
	for _, q := range out.Data {
		ts, _ := time.Parse(time.RFC3339Nano, q.Time)
		entry := dns.QueryEntry{
			Time:   ts,
			Domain: q.Question.Name,
			Client: q.Client,
			Type:   q.Question.Type,
		}
		if q.ClientInfo != nil {
			entry.ClientName = q.ClientInfo.Name
		}
		switch {
		case strings.HasPrefix(q.Reason, "Filtered") || strings.HasPrefix(q.Reason, "Rewritten"):
			entry.Status = dns.StatusBlocked
			entry.Reason = q.Rule
			if entry.Reason == "" {
				entry.Reason = q.Reason
			}
		case q.Cached:
			entry.Status = dns.StatusCached
			entry.Reason = "cache"
		default:
			entry.Status = dns.StatusAllowed
			entry.Reason = q.Upstream
		}
		if filter.Client != "" && entry.Client != filter.Client {
			continue
		}
		if filter.Status != "" && entry.Status != filter.Status {
			continue
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// ---- user rules ↔ normalized list entries ----------------------------------

// parseRule maps one AdGuard user rule onto the normalized types.
func parseRule(rule string) dns.ListEntry {
	trimmed := strings.TrimSpace(rule)
	entry := dns.ListEntry{ID: rule, Domain: trimmed, Enabled: true, Type: dns.ListCustom}
	switch {
	case trimmed == "" || strings.HasPrefix(trimmed, "!") || strings.HasPrefix(trimmed, "#"):
		entry.Type = dns.ListCustom
	case strings.HasPrefix(trimmed, "@@||") && strings.HasSuffix(trimmed, "^"):
		entry.Type = dns.ListAllow
		entry.Domain = strings.TrimSuffix(strings.TrimPrefix(trimmed, "@@||"), "^")
	case strings.HasPrefix(trimmed, "||") && strings.HasSuffix(trimmed, "^"):
		entry.Type = dns.ListDeny
		entry.Domain = strings.TrimSuffix(strings.TrimPrefix(trimmed, "||"), "^")
	case strings.HasPrefix(trimmed, "@@/") && strings.HasSuffix(trimmed, "/"):
		entry.Type = dns.ListAllowRegex
		entry.Domain = strings.TrimSuffix(strings.TrimPrefix(trimmed, "@@/"), "/")
	case len(trimmed) > 2 && strings.HasPrefix(trimmed, "/") && strings.HasSuffix(trimmed, "/"):
		entry.Type = dns.ListDenyRegex
		entry.Domain = strings.Trim(trimmed, "/")
	}
	return entry
}

func formatRule(entry dns.ListEntry) (string, error) {
	switch entry.Type {
	case dns.ListAllow:
		return "@@||" + entry.Domain + "^", nil
	case dns.ListDeny:
		return "||" + entry.Domain + "^", nil
	case dns.ListAllowRegex:
		return "@@/" + entry.Domain + "/", nil
	case dns.ListDenyRegex:
		return "/" + entry.Domain + "/", nil
	case dns.ListCustom:
		return entry.Domain, nil
	}
	return "", dns.ErrUnsupported
}

func (c *Conn) userRules(ctx context.Context) ([]string, error) {
	var out struct {
		UserRules []string `json:"user_rules"`
	}
	if err := c.client.call(ctx, "GET", "/control/filtering/status", nil, &out); err != nil {
		return nil, err
	}
	return out.UserRules, nil
}

func (c *Conn) setUserRules(ctx context.Context, rules []string) error {
	return c.client.call(ctx, "POST", "/control/filtering/set_rules", map[string]any{"rules": rules}, nil)
}

func (c *Conn) Lists(ctx context.Context) ([]dns.ListEntry, error) {
	rules, err := c.userRules(ctx)
	if err != nil {
		return nil, err
	}
	entries := make([]dns.ListEntry, 0, len(rules))
	for _, rule := range rules {
		if strings.TrimSpace(rule) == "" {
			continue
		}
		entries = append(entries, parseRule(rule))
	}
	return entries, nil
}

func (c *Conn) AddListEntry(ctx context.Context, entry dns.ListEntry) error {
	rule, err := formatRule(entry)
	if err != nil {
		return err
	}
	rules, err := c.userRules(ctx)
	if err != nil {
		return err
	}
	for _, existing := range rules {
		if existing == rule {
			return nil // already present
		}
	}
	return c.setUserRules(ctx, append(rules, rule))
}

func (c *Conn) RemoveListEntry(ctx context.Context, entry dns.ListEntry) error {
	target := entry.ID
	if target == "" {
		var err error
		target, err = formatRule(entry)
		if err != nil {
			return err
		}
	}
	rules, err := c.userRules(ctx)
	if err != nil {
		return err
	}
	kept := make([]string, 0, len(rules))
	for _, rule := range rules {
		if rule != target {
			kept = append(kept, rule)
		}
	}
	if len(kept) == len(rules) {
		return fmt.Errorf("rule not found: %s", target)
	}
	return c.setUserRules(ctx, kept)
}

// ---- clients ---------------------------------------------------------------

type agClient struct {
	Name string   `json:"name"`
	IDs  []string `json:"ids"`
	Tags []string `json:"tags"`
}

func (c *Conn) Clients(ctx context.Context) ([]dns.Client, error) {
	var out struct {
		Clients []agClient `json:"clients"`
	}
	if err := c.client.call(ctx, "GET", "/control/clients", nil, &out); err != nil {
		return nil, err
	}
	clients := make([]dns.Client, 0, len(out.Clients))
	for _, entry := range out.Clients {
		clients = append(clients, dns.Client{
			ID:          entry.Name,
			Name:        entry.Name,
			Identifiers: entry.IDs,
			GroupIDs:    entry.Tags,
		})
	}
	return clients, nil
}

// Groups surfaces AdGuard's fixed client-tag vocabulary read-only.
func (c *Conn) Groups(ctx context.Context) ([]dns.Group, error) {
	var out struct {
		SupportedTags []string `json:"supported_tags"`
	}
	if err := c.client.call(ctx, "GET", "/control/clients", nil, &out); err != nil {
		return nil, err
	}
	groups := make([]dns.Group, 0, len(out.SupportedTags))
	for _, tag := range out.SupportedTags {
		groups = append(groups, dns.Group{ID: tag, Name: tag, Enabled: true})
	}
	return groups, nil
}

func (c *Conn) UpsertClient(ctx context.Context, client dns.Client) error {
	payload := agClient{Name: client.Name, IDs: client.Identifiers, Tags: client.GroupIDs}
	if payload.Name == "" {
		payload.Name = client.ID
	}
	if client.ID != "" && client.ID != client.Name {
		// Renaming: update keyed by previous name.
		return c.client.call(ctx, "POST", "/control/clients/update",
			map[string]any{"name": client.ID, "data": payload}, nil)
	}
	// Try update first; fall back to add for new clients.
	err := c.client.call(ctx, "POST", "/control/clients/update",
		map[string]any{"name": payload.Name, "data": payload}, nil)
	if err != nil {
		return c.client.call(ctx, "POST", "/control/clients/add", payload, nil)
	}
	return nil
}

func (c *Conn) DeleteClient(ctx context.Context, id string) error {
	return c.client.call(ctx, "POST", "/control/clients/delete", map[string]any{"name": id}, nil)
}

func (c *Conn) UpsertGroup(context.Context, dns.Group) error { return dns.ErrUnsupported }
func (c *Conn) DeleteGroup(context.Context, string) error    { return dns.ErrUnsupported }
