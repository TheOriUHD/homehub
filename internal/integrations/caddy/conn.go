package caddy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"homehub/internal/caddycfg"
	"homehub/internal/caddyfile"
	"homehub/internal/integration"
	"homehub/internal/metrics"
)

// CaddyfileState is the helper's GET /caddyfile envelope.
type CaddyfileState struct {
	Path     string `json:"path"`
	Text     string `json:"text"`
	Sha256   string `json:"sha256"`
	Writable bool   `json:"writable"`
}

func fetchCaddyfile(ctx context.Context, c *client) (*CaddyfileState, error) {
	var state CaddyfileState
	if err := c.do(ctx, http.MethodGet, "/caddyfile", nil, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

// SiteEntry is one editable site for the API layer.
type SiteEntry struct {
	Domain           string
	ExtraDomains     []string
	UpstreamScheme   string
	UpstreamHost     string
	UpstreamPort     string
	DNSProvider      string
	DNSArgs          string
	Encode           string
	HeaderUp         []caddyfile.HeaderUp
	InsecureUpstream bool
	// UnknownDirectives is read-only for callers — carried by updates.
	UnknownDirectives []string
}

// RawSite is a site block the splitter refused to model, shown verbatim.
type RawSite struct {
	Addresses []string
	Reason    string
	Raw       string
}

// SitesState is everything the manager screen needs, read live.
type SitesState struct {
	Path      string
	Writable  bool
	Refusal   string
	GlobalRaw string
	Entries   []SiteEntry
	RawSites  []RawSite
}

type Conn struct {
	inst   integration.Instance
	client *client
	deps   integration.Deps
}

func (c *Conn) Close() {}

// Sites reads the live file and splits it. No cache, ever: the Caddyfile is
// the source of truth, not the database.
func (c *Conn) Sites(ctx context.Context) (*SitesState, error) {
	state, err := fetchCaddyfile(ctx, c.client)
	if err != nil {
		return nil, err
	}
	doc := caddyfile.Split(state.Text)
	out := &SitesState{Path: state.Path, Writable: state.Writable, Refusal: doc.Refusal}
	for _, blk := range doc.Blocks {
		switch blk.Kind {
		case caddyfile.KindGlobal:
			out.GlobalRaw = blk.Raw
		case caddyfile.KindEntry:
			out.Entries = append(out.Entries, entryToSite(*blk.Entry))
		case caddyfile.KindRaw:
			out.RawSites = append(out.RawSites, RawSite{
				Addresses: blk.Addresses, Reason: blk.Reason, Raw: blk.Raw,
			})
		}
	}
	return out, nil
}

func entryToSite(e caddyfile.Entry) SiteEntry {
	site := SiteEntry{
		UpstreamScheme:    e.Upstream.Scheme,
		UpstreamHost:      e.Upstream.Host,
		UpstreamPort:      e.Upstream.Port,
		Encode:            e.Encode,
		HeaderUp:          e.HeaderUp,
		InsecureUpstream:  e.InsecureUpstream,
		UnknownDirectives: e.UnknownDirectives,
	}
	if len(e.Domains) > 0 {
		site.Domain = e.Domains[0]
		site.ExtraDomains = e.Domains[1:]
	}
	if e.DNSChallenge != nil {
		site.DNSProvider = e.DNSChallenge.Provider
		site.DNSArgs = e.DNSChallenge.Args
	}
	return site
}

// buildEntry validates the caller's fields (§8.4) into a renderable entry.
func buildEntry(site SiteEntry) (caddyfile.Entry, error) {
	var entry caddyfile.Entry
	primary, err := caddyfile.NormalizeDomain(site.Domain)
	if err != nil {
		return entry, err
	}
	domains := []string{primary}
	for _, extra := range site.ExtraDomains {
		d, err := caddyfile.NormalizeDomain(extra)
		if err != nil {
			return entry, err
		}
		for _, existing := range domains {
			if existing == d {
				return entry, fmt.Errorf("%s is listed twice", d)
			}
		}
		domains = append(domains, d)
	}
	if err := caddyfile.ValidateUpstreamHost(site.UpstreamHost); err != nil {
		return entry, err
	}
	if err := caddyfile.ValidatePort(site.UpstreamPort); err != nil {
		return entry, err
	}
	entry = caddyfile.Entry{
		Domains: domains,
		Upstream: caddyfile.Upstream{
			Scheme: strings.TrimSpace(site.UpstreamScheme),
			Host:   strings.TrimSpace(site.UpstreamHost),
			Port:   strings.TrimSpace(site.UpstreamPort),
		},
		Encode:           strings.TrimSpace(site.Encode),
		HeaderUp:         site.HeaderUp,
		InsecureUpstream: site.InsecureUpstream,
	}
	if strings.TrimSpace(site.DNSProvider) != "" {
		entry.DNSChallenge = &caddyfile.DNSChallenge{
			Provider: strings.TrimSpace(site.DNSProvider),
			// Args kept verbatim — usually an {env.*} placeholder.
			Args: strings.TrimSpace(site.DNSArgs),
		}
	}
	return entry, nil
}

// mutate is the single write path: fetch → check writable and refusal →
// apply → PUT with the sha256 from the fetch → return the fresh state.
func (c *Conn) mutate(ctx context.Context, apply func(caddyfile.Document) (caddyfile.Document, error)) (*SitesState, error) {
	state, err := fetchCaddyfile(ctx, c.client)
	if err != nil {
		return nil, err
	}
	if !state.Writable {
		return nil, &helperError{Status: http.StatusForbidden}
	}
	doc := caddyfile.Split(state.Text)
	if doc.Refusal != "" {
		return nil, errors.New(doc.Refusal)
	}
	doc, err = apply(doc)
	if err != nil {
		return nil, err
	}
	body := map[string]string{"text": caddyfile.Render(doc), "sha256": state.Sha256}
	if err := c.client.do(ctx, http.MethodPut, "/caddyfile", body, nil); err != nil {
		return nil, err
	}
	c.deps.Publish("sites-changed", nil)
	return c.Sites(ctx)
}

// CreateSite adds a site; Caddy validates the result before disk changes.
func (c *Conn) CreateSite(ctx context.Context, site SiteEntry) (*SitesState, error) {
	entry, err := buildEntry(site)
	if err != nil {
		return nil, err
	}
	return c.mutate(ctx, func(doc caddyfile.Document) (caddyfile.Document, error) {
		return caddyfile.Create(doc, entry)
	})
}

// UpdateSite replaces the site currently serving oldDomain.
func (c *Conn) UpdateSite(ctx context.Context, oldDomain string, site SiteEntry) (*SitesState, error) {
	entry, err := buildEntry(site)
	if err != nil {
		return nil, err
	}
	return c.mutate(ctx, func(doc caddyfile.Document) (caddyfile.Document, error) {
		return caddyfile.Update(doc, oldDomain, entry)
	})
}

// RemoveSite deletes the site serving the domain.
func (c *Conn) RemoveSite(ctx context.Context, domain string) (*SitesState, error) {
	return c.mutate(ctx, func(doc caddyfile.Document) (caddyfile.Document, error) {
		return caddyfile.Remove(doc, domain)
	})
}

// Served reads the projected running config and reports what Caddy is
// actually serving right now.
func (c *Conn) Served(ctx context.Context) (*caddycfg.Result, error) {
	var envelope struct {
		Config    map[string]any `json:"config"`
		Projected bool           `json:"projected"`
	}
	if err := c.client.do(ctx, http.MethodGet, "/config", nil, &envelope); err != nil {
		return nil, err
	}
	if envelope.Config == nil {
		return nil, errors.New("the helper answered without a config")
	}
	return caddycfg.ParseConfig(envelope.Config), nil
}

// ---- platform hooks ---------------------------------------------------------

// Health: helper up and Caddy reachable → connected; helper up but Caddy
// down → degraded (the file is still readable); helper down → unreachable.
func (c *Conn) Health(ctx context.Context) integration.Health {
	var health struct {
		Caddy string `json:"caddy"`
	}
	err := c.client.do(ctx, http.MethodGet, "/health", nil, &health)
	if err != nil {
		var he *helperError
		if errors.As(err, &he) && he.Status == http.StatusServiceUnavailable {
			return integration.Health{Status: integration.StatusDegraded, Message: "the helper is up but cannot reach Caddy"}
		}
		return integration.Health{Status: integration.StatusUnreachable, Message: err.Error()}
	}
	return integration.Health{Status: integration.StatusConnected}
}

func (c *Conn) Resources(ctx context.Context) ([]integration.Resource, error) {
	return nil, nil
}

func (c *Conn) Collect(ctx context.Context) ([]metrics.Sample, error) {
	sites, err := c.Sites(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	res := TypeID + "/" + c.inst.ID.String()
	return []metrics.Sample{
		{Resource: res, Metric: "sites_managed", Unit: "count", TS: now, Value: float64(len(sites.Entries))},
		{Resource: res, Metric: "sites_total", Unit: "count", TS: now, Value: float64(len(sites.Entries) + len(sites.RawSites))},
	}, nil
}
