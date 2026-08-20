package api

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"homehub/internal/caddyfile"
	"homehub/internal/httpctx"
	"homehub/internal/integration"
	"homehub/internal/integrations/caddy"
	"homehub/internal/rbac"
	"homehub/internal/store"
)

// The Cloudflare half of the Caddy module: link a zone to a Caddy instance,
// manage its DNS records, and "publish" — DNS record + Caddy site in one step.

// CaddyCfLink verifies a Cloudflare token and stores the link. See the spec
// for the two-phase zone-picking behaviour.
func (s *Server) CaddyCfLink(ctx context.Context, req CaddyCfLinkRequestObject) (CaddyCfLinkResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	row, err := s.store.GetIntegration(ctx, req.Id)
	if err != nil || row.Type != caddy.TypeID {
		return CaddyCfLink404JSONResponse{notFoundErr("system")}, nil
	}
	resource := caddy.TypeID + "/" + req.Id.String()
	if !s.can(ctx, id, resource, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "caddy.cf_link", resource)
		return CaddyCfLink403JSONResponse{forbidden()}, nil
	}

	// Token: the body's, or the stored one for re-configuration.
	token := ""
	if req.Body.Token != nil {
		token = strings.TrimSpace(*req.Body.Token)
	}
	tokenProvided := token != ""
	if !tokenProvided {
		raw, verr := s.vault.Open(ctx, "integration:"+req.Id.String(), caddy.CFSecretName)
		if verr != nil || len(raw) == 0 {
			return CaddyCfLink400JSONResponse{BadRequestJSONResponse(errBody("token_required", "a Cloudflare API token is required — create one with the Zone → DNS → Edit permission"))}, nil
		}
		token = string(raw)
	}

	vctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	zones, err := caddy.CFVerifyToken(vctx, token)
	if err != nil {
		s.audit.Log(ctx, actorFor(id), "caddy.cf_link", resource, "error", map[string]any{"error": err.Error()})
		return CaddyCfLink400JSONResponse{BadRequestJSONResponse(errBody("token_invalid", err.Error()))}, nil
	}
	if len(zones) == 0 {
		return CaddyCfLink400JSONResponse{BadRequestJSONResponse(errBody("no_zones", "the token can't see any zones — grant it Zone → DNS → Edit on the zone you want to manage"))}, nil
	}

	cfg := integration.ConfigFromJSON(row.Config)
	existing := caddy.CFLinkFromConfig(cfg)

	// Resolve the zone: explicit id > the already-linked zone > a lone zone.
	zoneID := ""
	if req.Body.ZoneId != nil {
		zoneID = strings.TrimSpace(*req.Body.ZoneId)
	}
	if zoneID == "" && existing.ZoneID != "" {
		zoneID = existing.ZoneID
	}
	if zoneID == "" && len(zones) == 1 {
		zoneID = zones[0].ID
	}
	if zoneID == "" {
		// Several zones, none chosen — hand back the choices, store nothing.
		out := CaddyCfLinkResult{Linked: false}
		choices := make([]struct {
			Id   string `json:"id"`
			Name string `json:"name"`
		}, 0, len(zones))
		for _, z := range zones {
			choices = append(choices, struct {
				Id   string `json:"id"`
				Name string `json:"name"`
			}{Id: z.ID, Name: z.Name})
		}
		out.Zones = &choices
		return CaddyCfLink200JSONResponse(out), nil
	}
	zoneName := ""
	for _, z := range zones {
		if z.ID == zoneID {
			zoneName = z.Name
			break
		}
	}
	if zoneName == "" {
		return CaddyCfLink400JSONResponse{BadRequestJSONResponse(errBody("zone_unknown", "the token cannot manage that zone"))}, nil
	}

	// Merge the link into the existing config — never clobber baseUrl & co.
	cfg["cfZoneId"] = zoneID
	cfg["cfZoneName"] = zoneName
	if req.Body.RecordContent != nil {
		cfg["cfRecordContent"] = strings.TrimSpace(*req.Body.RecordContent)
	} else if _, ok := cfg["cfRecordContent"]; !ok {
		cfg["cfRecordContent"] = ""
	}
	if req.Body.Proxied != nil {
		cfg["cfProxied"] = *req.Body.Proxied
	} else if _, ok := cfg["cfProxied"]; !ok {
		cfg["cfProxied"] = false
	}
	if err := s.store.UpdateIntegration(ctx, row.ID, row.Name, cfg, row.Enabled); err != nil {
		return nil, err
	}
	if tokenProvided {
		if err := s.vault.Seal(ctx, "integration:"+row.ID.String(), caddy.CFSecretName, []byte(token)); err != nil {
			return nil, err
		}
	}
	s.integrations.Apply(ctx, row.ID)
	s.audit.Log(ctx, actorFor(id), "caddy.cf_link", resource, "success", map[string]any{"zone": zoneName})

	fresh := caddy.CFLinkFromConfig(cfg)
	out := CaddyCfLinkResult{Linked: true}
	out.ZoneName = &fresh.ZoneName
	out.RecordContent = &fresh.RecordContent
	out.Proxied = &fresh.ProxiedDefault
	return CaddyCfLink200JSONResponse(out), nil
}

func (s *Server) CaddyCfUnlink(ctx context.Context, req CaddyCfUnlinkRequestObject) (CaddyCfUnlinkResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	row, err := s.store.GetIntegration(ctx, req.Id)
	if err != nil || row.Type != caddy.TypeID {
		return CaddyCfUnlink404JSONResponse{notFoundErr("system")}, nil
	}
	resource := caddy.TypeID + "/" + req.Id.String()
	if !s.can(ctx, id, resource, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "caddy.cf_unlink", resource)
		return CaddyCfUnlink403JSONResponse{forbidden()}, nil
	}
	cfg := integration.ConfigFromJSON(row.Config)
	delete(cfg, "cfZoneId")
	delete(cfg, "cfZoneName")
	delete(cfg, "cfRecordContent")
	delete(cfg, "cfProxied")
	if err := s.store.UpdateIntegration(ctx, row.ID, row.Name, cfg, row.Enabled); err != nil {
		return nil, err
	}
	_ = s.vault.Delete(ctx, "integration:"+row.ID.String(), caddy.CFSecretName)
	s.integrations.Apply(ctx, row.ID)
	s.audit.Log(ctx, actorFor(id), "caddy.cf_unlink", resource, "success", nil)
	return CaddyCfUnlink204Response{}, nil
}

// ---- DNS reads ----------------------------------------------------------------

// dnsRelevant are the record types that can route a name at the proxy.
func dnsRelevant(recType string) bool {
	switch recType {
	case "A", "AAAA", "CNAME":
		return true
	}
	return false
}

func (s *Server) CaddyDns(ctx context.Context, req CaddyDnsRequestObject) (CaddyDnsResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, _, err := s.caddyConn(ctx, id, req.Id)
	if err == store.ErrNotFound {
		return CaddyDns404JSONResponse{notFoundErr("system")}, nil
	}
	if err != nil {
		return CaddyDns502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	link := conn.CFLink()
	if !link.Linked() {
		return CaddyDns400JSONResponse{BadRequestJSONResponse(errBody("not_linked", "this Caddy instance is not linked to Cloudflare"))}, nil
	}
	records, err := conn.CFRecords(ctx)
	if err != nil {
		return CaddyDns502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}

	// The Caddy side, for the route column — best-effort: a helper outage
	// still shows the records, just unmatched.
	routes := map[string]string{} // lowercase domain -> upstream host:port
	if sites, serr := conn.Sites(ctx); serr == nil {
		for _, e := range sites.Entries {
			up := e.UpstreamHost
			if e.UpstreamPort != "" {
				up += ":" + e.UpstreamPort
			}
			for _, d := range append([]string{e.Domain}, e.ExtraDomains...) {
				routes[strings.ToLower(d)] = up
			}
		}
	}

	zone := strings.ToLower(link.ZoneName)
	out := CaddyDnsState{
		ZoneName:       link.ZoneName,
		RecordContent:  link.RecordContent,
		ProxiedDefault: link.ProxiedDefault,
		Records:        []CaddyDnsRecord{},
		MissingDns:     []string{},
	}
	recordNames := map[string]bool{}
	for _, r := range records {
		rec := CaddyDnsRecord{Id: r.ID, Type: r.Type, Name: r.Name, Content: r.Content, Proxied: r.Proxied, Ttl: r.TTL}
		if dnsRelevant(r.Type) {
			recordNames[strings.ToLower(r.Name)] = true
			if up, ok := routes[strings.ToLower(r.Name)]; ok {
				d := r.Name
				rec.RouteDomain = &d
				rec.RouteUpstream = &up
			}
		}
		out.Records = append(out.Records, rec)
	}
	// Routing-relevant types first, then by name — the useful sort.
	sort.SliceStable(out.Records, func(i, j int) bool {
		ri, rj := dnsRelevant(out.Records[i].Type), dnsRelevant(out.Records[j].Type)
		if ri != rj {
			return ri
		}
		return out.Records[i].Name < out.Records[j].Name
	})
	// Caddy domains inside the zone that have no DNS record yet.
	for domain := range routes {
		if (domain == zone || strings.HasSuffix(domain, "."+zone)) && !recordNames[domain] {
			out.MissingDns = append(out.MissingDns, domain)
		}
	}
	sort.Strings(out.MissingDns)
	return CaddyDns200JSONResponse(out), nil
}

// ---- DNS mutations --------------------------------------------------------------

// caddyDnsGate resolves the connection and enforces control level.
func (s *Server) caddyDnsGate(ctx context.Context, instID uuid.UUID, action string) (*caddy.Conn, string, int) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, "", 401
	}
	conn, resource, err := s.caddyConn(ctx, id, instID)
	if err == store.ErrNotFound {
		return nil, "", 404
	}
	if err != nil {
		return nil, "", 502
	}
	if !s.can(ctx, id, resource, rbac.LevelControl) {
		s.deniedAudit(ctx, id, action, resource)
		return nil, "", 403
	}
	return conn, resource, 0
}

func recordFromInput(in CaddyDnsRecordInput) caddy.CFRecord {
	rec := caddy.CFRecord{Name: in.Name}
	if in.Type != nil {
		rec.Type = strings.ToUpper(strings.TrimSpace(*in.Type))
	}
	if in.Content != nil {
		rec.Content = strings.TrimSpace(*in.Content)
	}
	if in.Proxied != nil {
		rec.Proxied = *in.Proxied
	}
	if in.Ttl != nil {
		rec.TTL = *in.Ttl
	}
	return rec
}

func toAPIDnsRecord(r caddy.CFRecord) CaddyDnsRecord {
	return CaddyDnsRecord{Id: r.ID, Type: r.Type, Name: r.Name, Content: r.Content, Proxied: r.Proxied, Ttl: r.TTL}
}

func (s *Server) CaddyDnsCreate(ctx context.Context, req CaddyDnsCreateRequestObject) (CaddyDnsCreateResponseObject, error) {
	conn, resource, code := s.caddyDnsGate(ctx, req.Id, "caddy.dns_create")
	switch code {
	case 401:
		return nil, ErrUnauthenticated
	case 403:
		return CaddyDnsCreate403JSONResponse{forbidden()}, nil
	case 404:
		return CaddyDnsCreate404JSONResponse{notFoundErr("system")}, nil
	case 502:
		return CaddyDnsCreate502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", "connection unavailable"))}, nil
	}
	rec, err := conn.CFCreateRecord(ctx, recordFromInput(*req.Body))
	if err != nil {
		s.audit.Log(ctx, actorFor(httpctx.IdentityFrom(ctx)), "caddy.dns_create", resource, "error", map[string]any{"error": err.Error()})
		return CaddyDnsCreate400JSONResponse{BadRequestJSONResponse(errBody("rejected", err.Error()))}, nil
	}
	s.audit.Log(ctx, actorFor(httpctx.IdentityFrom(ctx)), "caddy.dns_create", resource, "success",
		map[string]any{"name": rec.Name, "type": rec.Type, "content": rec.Content})
	return CaddyDnsCreate200JSONResponse(toAPIDnsRecord(*rec)), nil
}

func (s *Server) CaddyDnsUpdate(ctx context.Context, req CaddyDnsUpdateRequestObject) (CaddyDnsUpdateResponseObject, error) {
	conn, resource, code := s.caddyDnsGate(ctx, req.Id, "caddy.dns_update")
	switch code {
	case 401:
		return nil, ErrUnauthenticated
	case 403:
		return CaddyDnsUpdate403JSONResponse{forbidden()}, nil
	case 404:
		return CaddyDnsUpdate404JSONResponse{notFoundErr("system")}, nil
	case 502:
		return CaddyDnsUpdate502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", "connection unavailable"))}, nil
	}
	rec, err := conn.CFUpdateRecord(ctx, req.RecordId, recordFromInput(*req.Body))
	if err != nil {
		s.audit.Log(ctx, actorFor(httpctx.IdentityFrom(ctx)), "caddy.dns_update", resource, "error", map[string]any{"error": err.Error()})
		return CaddyDnsUpdate400JSONResponse{BadRequestJSONResponse(errBody("rejected", err.Error()))}, nil
	}
	s.audit.Log(ctx, actorFor(httpctx.IdentityFrom(ctx)), "caddy.dns_update", resource, "success",
		map[string]any{"name": rec.Name, "type": rec.Type, "content": rec.Content})
	return CaddyDnsUpdate200JSONResponse(toAPIDnsRecord(*rec)), nil
}

func (s *Server) CaddyDnsDelete(ctx context.Context, req CaddyDnsDeleteRequestObject) (CaddyDnsDeleteResponseObject, error) {
	conn, resource, code := s.caddyDnsGate(ctx, req.Id, "caddy.dns_delete")
	switch code {
	case 401:
		return nil, ErrUnauthenticated
	case 403:
		return CaddyDnsDelete403JSONResponse{forbidden()}, nil
	case 404:
		return CaddyDnsDelete404JSONResponse{notFoundErr("system")}, nil
	case 502:
		return CaddyDnsDelete502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", "connection unavailable"))}, nil
	}
	if err := conn.CFDeleteRecord(ctx, req.RecordId); err != nil {
		s.audit.Log(ctx, actorFor(httpctx.IdentityFrom(ctx)), "caddy.dns_delete", resource, "error", map[string]any{"error": err.Error()})
		return CaddyDnsDelete400JSONResponse{BadRequestJSONResponse(errBody("rejected", err.Error()))}, nil
	}
	s.audit.Log(ctx, actorFor(httpctx.IdentityFrom(ctx)), "caddy.dns_delete", resource, "success",
		map[string]any{"recordId": req.RecordId})
	return CaddyDnsDelete204Response{}, nil
}

// ---- publish (the one-shot) ------------------------------------------------------

func (s *Server) CaddyPublish(ctx context.Context, req CaddyPublishRequestObject) (CaddyPublishResponseObject, error) {
	conn, resource, code := s.caddyDnsGate(ctx, req.Id, "caddy.publish")
	switch code {
	case 401:
		return nil, ErrUnauthenticated
	case 403:
		return CaddyPublish403JSONResponse{forbidden()}, nil
	case 404:
		return CaddyPublish404JSONResponse{notFoundErr("system")}, nil
	case 502:
		return CaddyPublish502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", "connection unavailable"))}, nil
	}
	link := conn.CFLink()

	// The published FQDN: resolved against the linked zone, or taken verbatim
	// (a full domain) when no zone is linked.
	name := strings.TrimSpace(req.Body.Name)
	var domain string
	if link.Linked() {
		domain = caddy.CFNormalizeName(name, link.ZoneName)
	} else {
		domain = strings.ToLower(strings.TrimSuffix(name, "."))
		if !strings.Contains(domain, ".") {
			return CaddyPublish400JSONResponse{BadRequestJSONResponse(errBody("bad_input", "without a Cloudflare link, provide a full domain (e.g. app.example.com)"))}, nil
		}
	}

	// Validate the Caddy half BEFORE touching DNS, so a typo'd upstream never
	// leaves a stray record behind.
	if _, err := caddyfile.NormalizeDomain(domain); err != nil {
		return CaddyPublish400JSONResponse{BadRequestJSONResponse(errBody("bad_input", err.Error()))}, nil
	}
	if err := caddyfile.ValidateUpstreamHost(req.Body.UpstreamHost); err != nil {
		return CaddyPublish400JSONResponse{BadRequestJSONResponse(errBody("bad_input", err.Error()))}, nil
	}
	port := ""
	if req.Body.UpstreamPort != nil {
		port = strings.TrimSpace(*req.Body.UpstreamPort)
	}
	if port != "" {
		if err := caddyfile.ValidatePort(port); err != nil {
			return CaddyPublish400JSONResponse{BadRequestJSONResponse(errBody("bad_input", err.Error()))}, nil
		}
	}

	out := CaddyPublishResult{Domain: domain}

	// DNS half.
	createDns := link.Linked()
	if req.Body.CreateDns != nil {
		createDns = *req.Body.CreateDns && link.Linked()
	}
	switch {
	case !link.Linked():
		out.DnsStatus = CaddyPublishResultDnsStatus("unlinked")
		out.DnsMessage = "no Cloudflare link — DNS untouched"
	case !createDns:
		out.DnsStatus = CaddyPublishResultDnsStatus("skipped")
		out.DnsMessage = "DNS creation skipped"
	default:
		existing := ""
		if records, rerr := conn.CFRecords(ctx); rerr == nil {
			for _, r := range records {
				if dnsRelevant(r.Type) && strings.EqualFold(r.Name, domain) {
					existing = fmt.Sprintf("%s → %s", r.Type, r.Content)
					break
				}
			}
		}
		if existing != "" {
			out.DnsStatus = CaddyPublishResultDnsStatus("exists")
			out.DnsMessage = "record already exists (" + existing + ")"
		} else {
			rec := caddy.CFRecord{Name: domain, Proxied: link.ProxiedDefault}
			if req.Body.DnsContent != nil {
				rec.Content = strings.TrimSpace(*req.Body.DnsContent)
			}
			if req.Body.Proxied != nil {
				rec.Proxied = *req.Body.Proxied
			}
			created, cerr := conn.CFCreateRecord(ctx, rec)
			if cerr != nil {
				out.DnsStatus = CaddyPublishResultDnsStatus("error")
				out.DnsMessage = cerr.Error()
			} else {
				out.DnsStatus = CaddyPublishResultDnsStatus("created")
				out.DnsMessage = fmt.Sprintf("%s → %s", created.Type, created.Content)
			}
		}
	}

	// Caddy half.
	site := caddy.SiteEntry{Domain: domain, UpstreamHost: strings.TrimSpace(req.Body.UpstreamHost), UpstreamPort: port}
	if req.Body.UpstreamScheme != nil {
		site.UpstreamScheme = strings.TrimSpace(*req.Body.UpstreamScheme)
	}
	mctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	state, serr := conn.CreateSite(mctx, site)
	if serr != nil {
		out.SiteStatus = CaddyPublishResultSiteStatus("error")
		out.SiteMessage = serr.Error()
	} else {
		out.SiteStatus = CaddyPublishResultSiteStatus("created")
		out.SiteMessage = "site created and validated by Caddy"
		st := toAPICaddyState(state)
		out.Sites = &st
	}
	s.audit.Log(ctx, actorFor(httpctx.IdentityFrom(ctx)), "caddy.publish", resource,
		map[bool]string{true: "success", false: "error"}[serr == nil],
		map[string]any{"domain": domain, "dns": string(out.DnsStatus), "site": string(out.SiteStatus)})
	return CaddyPublish200JSONResponse(out), nil
}
