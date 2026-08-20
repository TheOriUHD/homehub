package pihole

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/TheOriUHD/homehub/internal/integrations/dns"
)

// v5Client speaks the legacy /admin/api.php interface, authenticated with
// the API token on every request. It covers what that API exposes: stats,
// query log, the four lists, and enable/disable. Group and client CRUD do
// not exist in the v5 HTTP API and report as unsupported.
type v5Client struct {
	base  string
	token string
	http  *http.Client
}

func (c *v5Client) get(ctx context.Context, params url.Values, out any) error {
	params.Set("auth", c.token)
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/admin/api.php?"+params.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("Pi-hole request failed (%d)", resp.StatusCode)
	}
	// api.php answers "[]" to unauthorized requests instead of an error.
	if string(raw) == "[]" {
		return fmt.Errorf("Pi-hole rejected the request — check the API token (Settings → API → Show API token)")
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("unexpected Pi-hole response: %w", err)
		}
	}
	return nil
}

func (c *v5Client) Capabilities() dns.Capabilities {
	return dns.Capabilities{RegexLists: true}
}

func (c *v5Client) Version(ctx context.Context) (string, error) {
	var out struct {
		CoreCurrent string `json:"core_current"`
	}
	if err := c.get(ctx, url.Values{"versions": {""}}, &out); err != nil {
		return "", err
	}
	if out.CoreCurrent == "" {
		return "v5", nil
	}
	return out.CoreCurrent, nil
}

func (c *v5Client) Stats(ctx context.Context) (*dns.Stats, error) {
	var summary struct {
		QueriesToday   json.Number `json:"dns_queries_today"`
		BlockedToday   json.Number `json:"ads_blocked_today"`
		BlockedPct     json.Number `json:"ads_percentage_today"`
		UniqueClients  json.Number `json:"unique_clients"`
		DomainsBlocked json.Number `json:"domains_being_blocked"`
		Status         string      `json:"status"`
	}
	if err := c.get(ctx, url.Values{"summaryRaw": {""}}, &summary); err != nil {
		return nil, err
	}
	toI := func(n json.Number) int64 { v, _ := n.Int64(); return v }
	toF := func(n json.Number) float64 { v, _ := n.Float64(); return v }
	version, _ := c.Version(ctx)
	return &dns.Stats{
		QueriesToday:   toI(summary.QueriesToday),
		BlockedToday:   toI(summary.BlockedToday),
		BlockedPct:     toF(summary.BlockedPct),
		ClientsSeen:    toI(summary.UniqueClients),
		DomainsOnLists: toI(summary.DomainsBlocked),
		Enabled:        summary.Status == "enabled",
		Version:        version,
	}, nil
}

func (c *v5Client) Blocking(ctx context.Context) (*dns.BlockingState, error) {
	var out struct {
		Status string `json:"status"`
	}
	if err := c.get(ctx, url.Values{"status": {""}}, &out); err != nil {
		return nil, err
	}
	// v5 does not report remaining disable time.
	return &dns.BlockingState{Enabled: out.Status == "enabled"}, nil
}

func (c *v5Client) SetBlocking(ctx context.Context, enabled bool, duration time.Duration) error {
	params := url.Values{}
	if enabled {
		params.Set("enable", "")
	} else if duration > 0 {
		params.Set("disable", fmt.Sprint(int(duration.Seconds())))
	} else {
		params.Set("disable", "")
	}
	var out struct {
		Status string `json:"status"`
	}
	return c.get(ctx, params, &out)
}

// v5 status codes from FTL's querylog.
func normalizeV5Status(code string) (dns.QueryStatus, string) {
	switch code {
	case "2", "3":
		if code == "3" {
			return dns.StatusCached, "cache"
		}
		return dns.StatusAllowed, "forwarded"
	case "12", "13", "14":
		return dns.StatusAllowed, "retried"
	case "1":
		return dns.StatusBlocked, "gravity"
	case "4":
		return dns.StatusBlocked, "regex"
	case "5":
		return dns.StatusBlocked, "denylist"
	case "9", "10", "11":
		return dns.StatusBlocked, "gravity (CNAME)"
	case "6", "7", "8":
		return dns.StatusBlocked, "upstream blocked"
	}
	return dns.StatusAllowed, ""
}

func (c *v5Client) Queries(ctx context.Context, filter dns.QueryFilter) ([]dns.QueryEntry, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	var out struct {
		Data [][]string `json:"data"`
	}
	if err := c.get(ctx, url.Values{"getAllQueries": {fmt.Sprint(limit)}}, &out); err != nil {
		return nil, err
	}
	entries := make([]dns.QueryEntry, 0, len(out.Data))
	// Rows arrive oldest-first: [timestamp, type, domain, client, status, …]
	for i := len(out.Data) - 1; i >= 0; i-- {
		row := out.Data[i]
		if len(row) < 5 {
			continue
		}
		var ts int64
		_, _ = fmt.Sscan(row[0], &ts)
		status, reason := normalizeV5Status(row[4])
		entry := dns.QueryEntry{
			Time:   time.Unix(ts, 0),
			Type:   row[1],
			Domain: row[2],
			Client: row[3],
			Status: status,
			Reason: reason,
		}
		if filter.Search != "" && !strings.Contains(entry.Domain, filter.Search) {
			continue
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

var v5ListNames = map[dns.ListType]string{
	dns.ListAllow:      "white",
	dns.ListDeny:       "black",
	dns.ListAllowRegex: "regex_white",
	dns.ListDenyRegex:  "regex_black",
}

func (c *v5Client) Lists(ctx context.Context) ([]dns.ListEntry, error) {
	entries := []dns.ListEntry{}
	for listType, name := range v5ListNames {
		var out struct {
			Data []struct {
				ID      int64  `json:"id"`
				Domain  string `json:"domain"`
				Enabled int    `json:"enabled"`
				Comment string `json:"comment"`
			} `json:"data"`
		}
		if err := c.get(ctx, url.Values{"list": {name}}, &out); err != nil {
			return nil, err
		}
		for _, d := range out.Data {
			entries = append(entries, dns.ListEntry{
				ID:      fmt.Sprint(d.ID),
				Type:    listType,
				Domain:  d.Domain,
				Enabled: d.Enabled == 1,
				Comment: d.Comment,
			})
		}
	}
	return entries, nil
}

func (c *v5Client) AddListEntry(ctx context.Context, entry dns.ListEntry) error {
	name, ok := v5ListNames[entry.Type]
	if !ok {
		return dns.ErrUnsupported
	}
	return c.get(ctx, url.Values{"list": {name}, "add": {entry.Domain}}, nil)
}

func (c *v5Client) RemoveListEntry(ctx context.Context, entry dns.ListEntry) error {
	name, ok := v5ListNames[entry.Type]
	if !ok {
		return dns.ErrUnsupported
	}
	return c.get(ctx, url.Values{"list": {name}, "sub": {entry.Domain}}, nil)
}

// Clients are read-only in v5: derived from the query-sources top list.
func (c *v5Client) Clients(ctx context.Context) ([]dns.Client, error) {
	var out struct {
		TopSources map[string]int64 `json:"top_sources"`
	}
	if err := c.get(ctx, url.Values{"getQuerySources": {"25"}}, &out); err != nil {
		return nil, err
	}
	clients := make([]dns.Client, 0, len(out.TopSources))
	for source := range out.TopSources {
		// Sources arrive as "hostname|ip" or bare ip.
		name, ip := "", source
		if idx := strings.IndexByte(source, '|'); idx >= 0 {
			name, ip = source[:idx], source[idx+1:]
		}
		clients = append(clients, dns.Client{ID: ip, Name: name, Identifiers: []string{ip}})
	}
	return clients, nil
}

func (c *v5Client) Groups(ctx context.Context) ([]dns.Group, error) { return []dns.Group{}, nil }
func (c *v5Client) UpsertClient(context.Context, dns.Client) error  { return dns.ErrUnsupported }
func (c *v5Client) DeleteClient(context.Context, string) error      { return dns.ErrUnsupported }
func (c *v5Client) UpsertGroup(context.Context, dns.Group) error    { return dns.ErrUnsupported }
func (c *v5Client) DeleteGroup(context.Context, string) error       { return dns.ErrUnsupported }

// Overview pulls the dashboard extras from the legacy api.php interface.
func (c *v5Client) Overview(ctx context.Context) (*dns.Overview, error) {
	out := &dns.Overview{
		Series: []dns.SeriesPoint{}, TopDomains: []dns.TopItem{}, TopBlocked: []dns.TopItem{},
		TopClients: []dns.TopItem{}, TopUpstreams: []dns.TopItem{},
	}
	var top struct {
		TopQueries map[string]int64 `json:"top_queries"`
		TopAds     map[string]int64 `json:"top_ads"`
	}
	if err := c.get(ctx, url.Values{"topItems": {"8"}}, &top); err == nil {
		out.TopDomains = mapItems(top.TopQueries)
		out.TopBlocked = mapItems(top.TopAds)
	}
	var sources struct {
		TopSources map[string]int64 `json:"top_sources"`
	}
	if err := c.get(ctx, url.Values{"getQuerySources": {"8"}}, &sources); err == nil {
		out.TopClients = mapItems(sources.TopSources)
	}
	var over struct {
		Domains map[string]int64 `json:"domains_over_time"`
		Ads     map[string]int64 `json:"ads_over_time"`
	}
	if err := c.get(ctx, url.Values{"overTimeData10mins": {""}}, &over); err == nil {
		keys := make([]string, 0, len(over.Domains))
		for k := range over.Domains {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			ts, err := strconv.ParseInt(k, 10, 64)
			if err != nil {
				continue
			}
			out.Series = append(out.Series, dns.SeriesPoint{
				TS: time.Unix(ts, 0), Queries: over.Domains[k], Blocked: over.Ads[k],
			})
		}
	}
	return out, nil
}

// mapItems flattens v5's {"name": count} maps into sorted leaderboards.
func mapItems(m map[string]int64) []dns.TopItem {
	out := make([]dns.TopItem, 0, len(m))
	for name, count := range m {
		// v5 keys client rows as "hostname|ip"; keep the hostname half.
		if i := strings.IndexByte(name, '|'); i > 0 {
			name = name[:i]
		}
		out = append(out, dns.TopItem{Name: name, Count: count})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	return out
}
