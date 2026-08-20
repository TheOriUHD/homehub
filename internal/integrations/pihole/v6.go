package pihole

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"homehub/internal/integrations/dns"
)

// v6Client speaks the Pi-hole v6 REST API (/api). Authentication exchanges
// the app password for a short-lived session id, refreshed on 401.
type v6Client struct {
	base     string
	password string
	http     *http.Client

	mu  sync.Mutex
	sid string
}

func (c *v6Client) authenticate(ctx context.Context) error {
	payload, _ := json.Marshal(map[string]string{"password": c.password})
	req, err := http.NewRequestWithContext(ctx, "POST", c.base+"/api/auth", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		if resp.StatusCode == 401 {
			return fmt.Errorf("Pi-hole rejected the password (401) — use the app password from Settings → Web interface/API")
		}
		return fmt.Errorf("Pi-hole auth failed (%d)", resp.StatusCode)
	}
	var out struct {
		Session struct {
			SID string `json:"sid"`
		} `json:"session"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return err
	}
	c.mu.Lock()
	c.sid = out.Session.SID
	c.mu.Unlock()
	return nil
}

// call performs an authenticated request, re-authenticating once on 401.
func (c *v6Client) call(ctx context.Context, method, path string, body any, out any) error {
	for attempt := 0; attempt < 2; attempt++ {
		c.mu.Lock()
		sid := c.sid
		c.mu.Unlock()
		if sid == "" {
			if err := c.authenticate(ctx); err != nil {
				return err
			}
			c.mu.Lock()
			sid = c.sid
			c.mu.Unlock()
		}

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
		req.Header.Set("X-FTL-SID", sid)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return err
		}
		if resp.StatusCode == 401 {
			resp.Body.Close()
			c.mu.Lock()
			c.sid = ""
			c.mu.Unlock()
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
			var apiErr struct {
				Error struct {
					Message string `json:"message"`
					Hint    string `json:"hint"`
				} `json:"error"`
			}
			if json.Unmarshal(raw, &apiErr) == nil && apiErr.Error.Message != "" {
				msg := apiErr.Error.Message
				if apiErr.Error.Hint != "" {
					msg += " — " + apiErr.Error.Hint
				}
				return fmt.Errorf("Pi-hole: %s (%d)", msg, resp.StatusCode)
			}
			return fmt.Errorf("Pi-hole request failed (%d)", resp.StatusCode)
		}
		if out != nil {
			return json.NewDecoder(resp.Body).Decode(out)
		}
		return nil
	}
	return fmt.Errorf("Pi-hole authentication kept failing — check the app password")
}

func (c *v6Client) Capabilities() dns.Capabilities {
	return dns.Capabilities{GroupCRUD: true, ClientCRUD: true, RegexLists: true, ListComment: true}
}

func (c *v6Client) Version(ctx context.Context) (string, error) {
	var out struct {
		Version struct {
			Core struct {
				Local struct {
					Version string `json:"version"`
				} `json:"local"`
			} `json:"core"`
		} `json:"version"`
	}
	if err := c.call(ctx, "GET", "/api/info/version", nil, &out); err != nil {
		return "", err
	}
	return out.Version.Core.Local.Version, nil
}

func (c *v6Client) Stats(ctx context.Context) (*dns.Stats, error) {
	var summary struct {
		Queries struct {
			Total          int64   `json:"total"`
			Blocked        int64   `json:"blocked"`
			PercentBlocked float64 `json:"percent_blocked"`
		} `json:"queries"`
		Clients struct {
			Active int64 `json:"active"`
			Total  int64 `json:"total"`
		} `json:"clients"`
		Gravity struct {
			DomainsBeingBlocked int64 `json:"domains_being_blocked"`
		} `json:"gravity"`
	}
	if err := c.call(ctx, "GET", "/api/stats/summary", nil, &summary); err != nil {
		return nil, err
	}
	blocking, err := c.Blocking(ctx)
	if err != nil {
		return nil, err
	}
	version, _ := c.Version(ctx)
	return &dns.Stats{
		QueriesToday:   summary.Queries.Total,
		BlockedToday:   summary.Queries.Blocked,
		BlockedPct:     summary.Queries.PercentBlocked,
		ClientsSeen:    summary.Clients.Active,
		DomainsOnLists: summary.Gravity.DomainsBeingBlocked,
		Enabled:        blocking.Enabled,
		DisabledUntil:  blocking.Until,
		Version:        version,
	}, nil
}

func (c *v6Client) Blocking(ctx context.Context) (*dns.BlockingState, error) {
	var out struct {
		Blocking string   `json:"blocking"`
		Timer    *float64 `json:"timer"`
	}
	if err := c.call(ctx, "GET", "/api/dns/blocking", nil, &out); err != nil {
		return nil, err
	}
	state := &dns.BlockingState{Enabled: out.Blocking == "enabled"}
	if out.Timer != nil && *out.Timer > 0 {
		until := time.Now().Add(time.Duration(*out.Timer * float64(time.Second)))
		state.Until = &until
	}
	return state, nil
}

func (c *v6Client) SetBlocking(ctx context.Context, enabled bool, duration time.Duration) error {
	payload := map[string]any{"blocking": enabled}
	if duration > 0 {
		payload["timer"] = int(duration.Seconds())
	} else {
		payload["timer"] = nil
	}
	return c.call(ctx, "POST", "/api/dns/blocking", payload, nil)
}

func (c *v6Client) Queries(ctx context.Context, filter dns.QueryFilter) ([]dns.QueryEntry, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	params := url.Values{"length": {fmt.Sprint(limit)}}
	if filter.Search != "" {
		params.Set("domain", "*"+filter.Search+"*")
	}
	if filter.Client != "" {
		params.Set("client_ip", filter.Client)
	}
	var out struct {
		Queries []struct {
			Time   float64 `json:"time"`
			Type   string  `json:"type"`
			Domain string  `json:"domain"`
			Status string  `json:"status"`
			Client struct {
				IP   string  `json:"ip"`
				Name *string `json:"name"`
			} `json:"client"`
			Upstream *string `json:"upstream"`
		} `json:"queries"`
	}
	if err := c.call(ctx, "GET", "/api/queries?"+params.Encode(), nil, &out); err != nil {
		return nil, err
	}
	entries := make([]dns.QueryEntry, 0, len(out.Queries))
	for _, q := range out.Queries {
		entry := dns.QueryEntry{
			Time:   time.Unix(int64(q.Time), 0),
			Domain: q.Domain,
			Client: q.Client.IP,
			Type:   q.Type,
			Status: normalizeV6Status(q.Status),
			Reason: q.Status,
		}
		if q.Client.Name != nil {
			entry.ClientName = *q.Client.Name
		}
		if q.Upstream != nil && entry.Status == dns.StatusAllowed {
			entry.Reason = *q.Upstream
		}
		if filter.Status != "" && entry.Status != filter.Status {
			continue
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func normalizeV6Status(status string) dns.QueryStatus {
	switch status {
	case "FORWARDED", "RETRIED", "RETRIED_DNSSEC", "IN_PROGRESS":
		return dns.StatusAllowed
	case "CACHE", "CACHE_STALE":
		return dns.StatusCached
	default:
		// GRAVITY, REGEX, DENYLIST, EXTERNAL_BLOCKED_*, GRAVITY_CNAME, …
		if strings.HasPrefix(status, "CACHE") {
			return dns.StatusCached
		}
		return dns.StatusBlocked
	}
}

func (c *v6Client) Lists(ctx context.Context) ([]dns.ListEntry, error) {
	var out struct {
		Domains []struct {
			ID      int64  `json:"id"`
			Domain  string `json:"domain"`
			Type    string `json:"type"`
			Kind    string `json:"kind"`
			Enabled bool   `json:"enabled"`
			Comment string `json:"comment"`
		} `json:"domains"`
	}
	if err := c.call(ctx, "GET", "/api/domains", nil, &out); err != nil {
		return nil, err
	}
	entries := make([]dns.ListEntry, 0, len(out.Domains))
	for _, d := range out.Domains {
		listType := dns.ListType(d.Type)
		if d.Kind == "regex" {
			listType = dns.ListType(d.Type + "_regex")
		}
		entries = append(entries, dns.ListEntry{
			ID:      fmt.Sprint(d.ID),
			Type:    listType,
			Domain:  d.Domain,
			Enabled: d.Enabled,
			Comment: d.Comment,
		})
	}
	return entries, nil
}

func v6ListPath(listType dns.ListType) (string, error) {
	switch listType {
	case dns.ListAllow:
		return "allow/exact", nil
	case dns.ListDeny:
		return "deny/exact", nil
	case dns.ListAllowRegex:
		return "allow/regex", nil
	case dns.ListDenyRegex:
		return "deny/regex", nil
	}
	return "", dns.ErrUnsupported
}

func (c *v6Client) AddListEntry(ctx context.Context, entry dns.ListEntry) error {
	path, err := v6ListPath(entry.Type)
	if err != nil {
		return err
	}
	payload := map[string]any{"domain": entry.Domain, "enabled": true}
	if entry.Comment != "" {
		payload["comment"] = entry.Comment
	}
	return c.call(ctx, "POST", "/api/domains/"+path, payload, nil)
}

func (c *v6Client) RemoveListEntry(ctx context.Context, entry dns.ListEntry) error {
	path, err := v6ListPath(entry.Type)
	if err != nil {
		return err
	}
	return c.call(ctx, "DELETE", "/api/domains/"+path+"/"+url.PathEscape(entry.Domain), nil, nil)
}

func (c *v6Client) Clients(ctx context.Context) ([]dns.Client, error) {
	var out struct {
		Clients []struct {
			ID      int64   `json:"id"`
			Client  string  `json:"client"`
			Comment *string `json:"comment"`
			Groups  []int64 `json:"groups"`
		} `json:"clients"`
	}
	if err := c.call(ctx, "GET", "/api/clients", nil, &out); err != nil {
		return nil, err
	}
	clients := make([]dns.Client, 0, len(out.Clients))
	for _, entry := range out.Clients {
		client := dns.Client{
			ID:          entry.Client,
			Identifiers: []string{entry.Client},
		}
		if entry.Comment != nil {
			client.Name = *entry.Comment
		}
		for _, g := range entry.Groups {
			client.GroupIDs = append(client.GroupIDs, fmt.Sprint(g))
		}
		clients = append(clients, client)
	}
	return clients, nil
}

func (c *v6Client) Groups(ctx context.Context) ([]dns.Group, error) {
	var out struct {
		Groups []struct {
			ID      int64  `json:"id"`
			Name    string `json:"name"`
			Enabled bool   `json:"enabled"`
		} `json:"groups"`
	}
	if err := c.call(ctx, "GET", "/api/groups", nil, &out); err != nil {
		return nil, err
	}
	groups := make([]dns.Group, 0, len(out.Groups))
	for _, g := range out.Groups {
		groups = append(groups, dns.Group{ID: fmt.Sprint(g.ID), Name: g.Name, Enabled: g.Enabled})
	}
	return groups, nil
}

func (c *v6Client) UpsertClient(ctx context.Context, client dns.Client) error {
	groups := make([]int64, 0, len(client.GroupIDs))
	for _, id := range client.GroupIDs {
		var n int64
		if _, err := fmt.Sscan(id, &n); err == nil {
			groups = append(groups, n)
		}
	}
	identifier := client.ID
	if identifier == "" && len(client.Identifiers) > 0 {
		identifier = client.Identifiers[0]
	}
	payload := map[string]any{"client": identifier, "comment": client.Name, "groups": groups}
	// The v6 API upserts via PUT on the identifier; creation uses POST.
	err := c.call(ctx, "PUT", "/api/clients/"+url.PathEscape(identifier), payload, nil)
	if err != nil && strings.Contains(err.Error(), "404") {
		return c.call(ctx, "POST", "/api/clients", payload, nil)
	}
	if err != nil {
		// Unknown identifier → create.
		return c.call(ctx, "POST", "/api/clients", payload, nil)
	}
	return nil
}

func (c *v6Client) DeleteClient(ctx context.Context, id string) error {
	return c.call(ctx, "DELETE", "/api/clients/"+url.PathEscape(id), nil, nil)
}

func (c *v6Client) UpsertGroup(ctx context.Context, group dns.Group) error {
	payload := map[string]any{"name": group.Name, "enabled": group.Enabled}
	if group.ID != "" {
		// Existing group: PUT by current name (id lookup by listing).
		groups, err := c.Groups(ctx)
		if err != nil {
			return err
		}
		for _, g := range groups {
			if g.ID == group.ID {
				return c.call(ctx, "PUT", "/api/groups/"+url.PathEscape(g.Name), payload, nil)
			}
		}
	}
	return c.call(ctx, "POST", "/api/groups", payload, nil)
}

func (c *v6Client) DeleteGroup(ctx context.Context, id string) error {
	groups, err := c.Groups(ctx)
	if err != nil {
		return err
	}
	for _, g := range groups {
		if g.ID == id {
			return c.call(ctx, "DELETE", "/api/groups/"+url.PathEscape(g.Name), nil, nil)
		}
	}
	return fmt.Errorf("group %s not found", id)
}

// Overview pulls the dashboard extras from the v6 REST API. Every call is
// best-effort — a missing endpoint yields an empty panel, not an error.
func (c *v6Client) Overview(ctx context.Context) (*dns.Overview, error) {
	out := &dns.Overview{
		Series: []dns.SeriesPoint{}, TopDomains: []dns.TopItem{}, TopBlocked: []dns.TopItem{},
		TopClients: []dns.TopItem{}, TopUpstreams: []dns.TopItem{},
	}

	var topAllowed struct {
		Domains []struct {
			Domain string `json:"domain"`
			Count  int64  `json:"count"`
		} `json:"domains"`
	}
	if err := c.call(ctx, "GET", "/api/stats/top_domains?count=8", nil, &topAllowed); err == nil {
		for _, d := range topAllowed.Domains {
			out.TopDomains = append(out.TopDomains, dns.TopItem{Name: d.Domain, Count: d.Count})
		}
	}
	var topBlocked struct {
		Domains []struct {
			Domain string `json:"domain"`
			Count  int64  `json:"count"`
		} `json:"domains"`
	}
	if err := c.call(ctx, "GET", "/api/stats/top_domains?blocked=true&count=8", nil, &topBlocked); err == nil {
		for _, d := range topBlocked.Domains {
			out.TopBlocked = append(out.TopBlocked, dns.TopItem{Name: d.Domain, Count: d.Count})
		}
	}
	var clients struct {
		Clients []struct {
			IP    string `json:"ip"`
			Name  string `json:"name"`
			Count int64  `json:"count"`
		} `json:"clients"`
	}
	if err := c.call(ctx, "GET", "/api/stats/top_clients?count=8", nil, &clients); err == nil {
		for _, cl := range clients.Clients {
			name := cl.Name
			if name == "" {
				name = cl.IP
			}
			out.TopClients = append(out.TopClients, dns.TopItem{Name: name, Count: cl.Count})
		}
	}
	var upstreams struct {
		Upstreams []struct {
			IP    string `json:"ip"`
			Name  string `json:"name"`
			Count int64  `json:"count"`
		} `json:"upstreams"`
	}
	if err := c.call(ctx, "GET", "/api/stats/upstreams", nil, &upstreams); err == nil {
		for _, u := range upstreams.Upstreams {
			name := u.Name
			if name == "" {
				name = u.IP
			}
			out.TopUpstreams = append(out.TopUpstreams, dns.TopItem{Name: name, Count: u.Count})
		}
	}
	var history struct {
		History []struct {
			Timestamp float64 `json:"timestamp"`
			Total     int64   `json:"total"`
			Blocked   int64   `json:"blocked"`
		} `json:"history"`
	}
	if err := c.call(ctx, "GET", "/api/history", nil, &history); err == nil {
		for _, h := range history.History {
			out.Series = append(out.Series, dns.SeriesPoint{
				TS: time.Unix(int64(h.Timestamp), 0), Queries: h.Total, Blocked: h.Blocked,
			})
		}
	}
	return out, nil
}
