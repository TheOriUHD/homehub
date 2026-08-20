package caddy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Cloudflare DNS link. A Caddy instance can optionally be linked to one
// Cloudflare zone so HomeHub manages both halves of the classic homelab
// pattern in one motion: a DNS record pointing the (sub)domain at the reverse
// proxy, and the Caddy site routing it to the actual service.
//
// The link is deliberately flexible so it fits any setup, not one person's:
//   - the token is a scoped API token (Zone.DNS Edit), stored in the vault as
//     the instance secret "cfToken" and never sent to the browser;
//   - the zone is chosen from whatever zones that token can touch;
//   - the default record content is configurable — a LAN IP for a
//     LAN-only Caddy, a public IP, or a DDNS hostname (type auto-detects to
//     A / AAAA / CNAME accordingly);
//   - the default proxied mode is configurable (grey-cloud for LAN IPs,
//     orange-cloud for setups routed through Cloudflare).
//
// Config keys on the integration row: cfZoneId, cfZoneName, cfRecordContent,
// cfProxied. All absent = not linked.

const cfAPIBase = "https://api.cloudflare.com/client/v4"

// CFSecretName is the vault entry holding the Cloudflare API token.
const CFSecretName = "cfToken"

// CFLink is the per-instance Cloudflare configuration, read from config.
type CFLink struct {
	ZoneID         string
	ZoneName       string
	RecordContent  string // default content for new records
	ProxiedDefault bool
}

// CFLinkFromConfig extracts the link from an integration config map.
func CFLinkFromConfig(cfg map[string]any) CFLink {
	link := CFLink{}
	link.ZoneID, _ = cfg["cfZoneId"].(string)
	link.ZoneName, _ = cfg["cfZoneName"].(string)
	link.RecordContent, _ = cfg["cfRecordContent"].(string)
	link.ProxiedDefault, _ = cfg["cfProxied"].(bool)
	return link
}

// Linked reports whether the instance has a usable Cloudflare link.
func (l CFLink) Linked() bool { return l.ZoneID != "" && l.ZoneName != "" }

// CFZone is one zone the token can manage.
type CFZone struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// CFRecord is one DNS record, normalized for the API layer.
type CFRecord struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Name    string `json:"name"` // FQDN as Cloudflare stores it
	Content string `json:"content"`
	Proxied bool   `json:"proxied"`
	TTL     int    `json:"ttl"` // 1 = auto
}

// cfClient is a minimal Cloudflare v4 API client (Bearer token).
type cfClient struct {
	token string
	http  *http.Client
}

func newCFClient(token string) *cfClient {
	return &cfClient{token: strings.TrimSpace(token), http: &http.Client{Timeout: 20 * time.Second}}
}

// cfEnvelope is Cloudflare's universal response wrapper.
type cfEnvelope struct {
	Success bool `json:"success"`
	Errors  []struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
	Result     json.RawMessage `json:"result"`
	ResultInfo *struct {
		Page       int `json:"page"`
		TotalPages int `json:"total_pages"`
	} `json:"result_info"`
}

func (c *cfClient) do(ctx context.Context, method, path string, body any, out any) (*cfEnvelope, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, cfAPIBase+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, friendlyCFNetErr(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, err
	}
	var env cfEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("Cloudflare answered %d with an unexpected body", resp.StatusCode)
	}
	if !env.Success {
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return nil, errors.New("Cloudflare rejected the API token — check that it is valid and has Zone → Zone → Read plus Zone → DNS → Edit on the zone")
		}
		if len(env.Errors) > 0 {
			return nil, fmt.Errorf("Cloudflare: %s", env.Errors[0].Message)
		}
		return nil, fmt.Errorf("Cloudflare request failed (%d)", resp.StatusCode)
	}
	if out != nil && len(env.Result) > 0 {
		if err := json.Unmarshal(env.Result, out); err != nil {
			return nil, fmt.Errorf("decode Cloudflare response: %w", err)
		}
	}
	return &env, nil
}

func friendlyCFNetErr(err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "no such host"), strings.Contains(msg, "connection refused"):
		return errors.New("cannot reach api.cloudflare.com — check the server's internet access")
	case strings.Contains(msg, "context deadline exceeded"), strings.Contains(msg, "Client.Timeout"):
		return errors.New("Cloudflare did not answer in time")
	}
	return err
}

// Zones lists every zone the token can see (paginated, capped defensively).
func (c *cfClient) Zones(ctx context.Context) ([]CFZone, error) {
	var all []CFZone
	for page := 1; page <= 5; page++ {
		var zones []CFZone
		env, err := c.do(ctx, http.MethodGet, fmt.Sprintf("/zones?per_page=50&page=%d", page), nil, &zones)
		if err != nil {
			return nil, err
		}
		all = append(all, zones...)
		if env.ResultInfo == nil || env.ResultInfo.Page >= env.ResultInfo.TotalPages {
			break
		}
	}
	return all, nil
}

// Records lists a zone's DNS records (paginated, capped at 2000 records).
func (c *cfClient) Records(ctx context.Context, zoneID string) ([]CFRecord, error) {
	var all []CFRecord
	for page := 1; page <= 10; page++ {
		var records []CFRecord
		env, err := c.do(ctx, http.MethodGet,
			fmt.Sprintf("/zones/%s/dns_records?per_page=200&page=%d", url.PathEscape(zoneID), page), nil, &records)
		if err != nil {
			return nil, err
		}
		all = append(all, records...)
		if env.ResultInfo == nil || env.ResultInfo.Page >= env.ResultInfo.TotalPages {
			break
		}
	}
	return all, nil
}

type cfRecordBody struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	Proxied bool   `json:"proxied"`
	TTL     int    `json:"ttl"`
}

func (c *cfClient) CreateRecord(ctx context.Context, zoneID string, rec CFRecord) (*CFRecord, error) {
	if rec.TTL == 0 {
		rec.TTL = 1 // auto
	}
	var out CFRecord
	_, err := c.do(ctx, http.MethodPost, "/zones/"+url.PathEscape(zoneID)+"/dns_records",
		cfRecordBody{Type: rec.Type, Name: rec.Name, Content: rec.Content, Proxied: rec.Proxied, TTL: rec.TTL}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *cfClient) UpdateRecord(ctx context.Context, zoneID, recordID string, rec CFRecord) (*CFRecord, error) {
	if rec.TTL == 0 {
		rec.TTL = 1
	}
	var out CFRecord
	_, err := c.do(ctx, http.MethodPut,
		"/zones/"+url.PathEscape(zoneID)+"/dns_records/"+url.PathEscape(recordID),
		cfRecordBody{Type: rec.Type, Name: rec.Name, Content: rec.Content, Proxied: rec.Proxied, TTL: rec.TTL}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *cfClient) DeleteRecord(ctx context.Context, zoneID, recordID string) error {
	_, err := c.do(ctx, http.MethodDelete,
		"/zones/"+url.PathEscape(zoneID)+"/dns_records/"+url.PathEscape(recordID), nil, nil)
	return err
}

// ---- helpers shared with the API layer ---------------------------------------

// CFVerifyToken lists the zones a raw token can manage — the linking flow.
func CFVerifyToken(ctx context.Context, token string) ([]CFZone, error) {
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("an API token is required")
	}
	return newCFClient(token).Zones(ctx)
}

// CFAutoType picks the record type for a content value: IPv4 → A, IPv6 →
// AAAA, anything else → CNAME.
func CFAutoType(content string) string {
	ip := net.ParseIP(strings.TrimSpace(content))
	switch {
	case ip == nil:
		return "CNAME"
	case ip.To4() != nil:
		return "A"
	default:
		return "AAAA"
	}
}

// CFNormalizeName resolves user input to the FQDN Cloudflare stores:
// "@" or "" → the zone apex; "amp" → "amp.<zone>"; an input already ending in
// the zone is kept. Lowercased, trailing dots stripped.
func CFNormalizeName(input, zone string) string {
	name := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(input), "."))
	zone = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(zone), "."))
	if name == "" || name == "@" || name == zone {
		return zone
	}
	if strings.HasSuffix(name, "."+zone) {
		return name
	}
	return name + "." + zone
}

// CFProxiable reports whether a record type can be orange-clouded — only
// A/AAAA/CNAME are; Cloudflare rejects proxied on everything else.
func CFProxiable(recType string) bool {
	switch strings.ToUpper(recType) {
	case "A", "AAAA", "CNAME":
		return true
	}
	return false
}

// ---- CFOps -------------------------------------------------------------------
//
// CFOps performs the Cloudflare operations for a linked instance. It is built
// from the stored link config + vault token DIRECTLY — deliberately not from
// the live helper connection — so DNS keeps working even while the caddy-reader
// host is down (which is exactly when an operator may want to repoint DNS).

type CFOps struct {
	link   CFLink
	client *cfClient
}

// NewCFOps builds the ops for a linked instance.
func NewCFOps(link CFLink, token string) (*CFOps, error) {
	if !link.Linked() {
		return nil, errors.New("this Caddy instance is not linked to Cloudflare")
	}
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("the Cloudflare link is missing its API token — re-link Cloudflare")
	}
	return &CFOps{link: link, client: newCFClient(token)}, nil
}

// Link exposes the ops' link config.
func (o *CFOps) Link() CFLink { return o.link }

// Records lists the linked zone's records.
func (o *CFOps) Records(ctx context.Context) ([]CFRecord, error) {
	return o.client.Records(ctx, o.link.ZoneID)
}

// CreateRecord creates a record, filling defaults from the link: empty
// content → the configured default target; empty type → auto from content.
// Proxied is stripped for types Cloudflare can't proxy.
func (o *CFOps) CreateRecord(ctx context.Context, rec CFRecord) (*CFRecord, error) {
	rec.Name = CFNormalizeName(rec.Name, o.link.ZoneName)
	if strings.TrimSpace(rec.Content) == "" {
		rec.Content = o.link.RecordContent
	}
	if strings.TrimSpace(rec.Content) == "" {
		return nil, errors.New("a record target is required — set a default target in the Cloudflare link settings or provide one")
	}
	if strings.TrimSpace(rec.Type) == "" {
		rec.Type = CFAutoType(rec.Content)
	}
	if !CFProxiable(rec.Type) {
		rec.Proxied = false
	}
	return o.client.CreateRecord(ctx, o.link.ZoneID, rec)
}

// UpdateRecord updates a record (full replace, Cloudflare semantics).
func (o *CFOps) UpdateRecord(ctx context.Context, recordID string, rec CFRecord) (*CFRecord, error) {
	rec.Name = CFNormalizeName(rec.Name, o.link.ZoneName)
	if strings.TrimSpace(rec.Type) == "" {
		rec.Type = CFAutoType(rec.Content)
	}
	if !CFProxiable(rec.Type) {
		rec.Proxied = false
	}
	return o.client.UpdateRecord(ctx, o.link.ZoneID, recordID, rec)
}

// DeleteRecord deletes a record from the linked zone.
func (o *CFOps) DeleteRecord(ctx context.Context, recordID string) error {
	return o.client.DeleteRecord(ctx, o.link.ZoneID, recordID)
}
