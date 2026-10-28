package api

import (
	"context"
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

// caddyConn resolves an integration id to a live Caddy connection, enforcing
// instance visibility.
func (s *Server) caddyConn(ctx context.Context, id *httpctx.Identity, instID uuid.UUID) (*caddy.Conn, string, error) {
	row, err := s.store.GetIntegration(ctx, instID)
	if err != nil || row.Type != caddy.TypeID {
		return nil, "", store.ErrNotFound
	}
	resource := row.Type + "/" + instID.String()
	if !s.visibleUnder(ctx, id, resource) {
		return nil, "", store.ErrNotFound
	}
	conn, err := s.integrations.Connection(instID)
	if err != nil {
		return nil, resource, err
	}
	cc, ok := conn.(*caddy.Conn)
	if !ok {
		return nil, resource, store.ErrNotFound
	}
	return cc, resource, nil
}

// CaddyPair redeems a six-digit code and creates the integration in one
// server-side step — the helper token never touches the browser.
func (s *Server) CaddyPair(ctx context.Context, req CaddyPairRequestObject) (CaddyPairResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResIntegrations, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "caddy.pair", ResIntegrations)
		return CaddyPair403JSONResponse{forbidden()}, nil
	}
	name := strings.TrimSpace(req.Body.Name)
	if name == "" {
		return CaddyPair400JSONResponse{BadRequestJSONResponse(errBody("invalid_name", "a name is required"))}, nil
	}

	pctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	token, err := caddy.Pair(pctx, req.Body.BaseUrl, req.Body.Code)
	if err != nil {
		s.audit.Log(ctx, actorFor(id), "caddy.pair", ResIntegrations, "error",
			map[string]any{"baseUrl": req.Body.BaseUrl, "error": err.Error()})
		return CaddyPair400JSONResponse{BadRequestJSONResponse(errBody("pairing_failed", err.Error()))}, nil
	}

	row, err := s.store.CreateIntegration(ctx, caddy.TypeID, name, map[string]any{"baseUrl": req.Body.BaseUrl})
	if err != nil {
		if store.IsUniqueViolation(err) {
			return CaddyPair409JSONResponse{ConflictJSONResponse(errBody("name_taken", "an integration with that name already exists"))}, nil
		}
		return nil, err
	}
	if err := s.vault.Seal(ctx, "integration:"+row.ID.String(), "token", []byte(token)); err != nil {
		return nil, err
	}
	s.integrations.Apply(ctx, row.ID)
	s.audit.Log(ctx, actorFor(id), "caddy.pair", caddy.TypeID+"/"+row.ID.String(), "success",
		map[string]any{"name": name, "baseUrl": req.Body.BaseUrl})

	fresh, err := s.store.GetIntegration(ctx, row.ID)
	if err != nil {
		return nil, err
	}
	return CaddyPair201JSONResponse(toAPIIntegration(*fresh)), nil
}

// CaddyRepair replaces the stored token with one from a fresh code.
func (s *Server) CaddyRepair(ctx context.Context, req CaddyRepairRequestObject) (CaddyRepairResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	row, err := s.store.GetIntegration(ctx, req.Id)
	if err != nil || row.Type != caddy.TypeID {
		return CaddyRepair404JSONResponse{notFoundErr("system")}, nil
	}
	resource := caddy.TypeID + "/" + req.Id.String()
	if !s.can(ctx, id, ResIntegrations, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "caddy.repair", resource)
		return CaddyRepair403JSONResponse{forbidden()}, nil
	}
	cfg := integration.ConfigFromJSON(row.Config)
	baseURL, _ := cfg["baseUrl"].(string)
	pctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	token, err := caddy.Pair(pctx, baseURL, req.Body.Code)
	if err != nil {
		s.audit.Log(ctx, actorFor(id), "caddy.repair", resource, "error", map[string]any{"error": err.Error()})
		return CaddyRepair400JSONResponse{BadRequestJSONResponse(errBody("pairing_failed", err.Error()))}, nil
	}
	if err := s.vault.Seal(ctx, "integration:"+req.Id.String(), "token", []byte(token)); err != nil {
		return nil, err
	}
	s.integrations.Apply(ctx, req.Id)
	s.audit.Log(ctx, actorFor(id), "caddy.repair", resource, "success", nil)
	return CaddyRepair204Response{}, nil
}

// ---- state mapping ----------------------------------------------------------

func toAPICaddySite(e caddy.SiteEntry) CaddySite {
	out := CaddySite{
		Domain:            e.Domain,
		UpstreamHost:      e.UpstreamHost,
		UnknownDirectives: e.UnknownDirectives,
	}
	if out.UnknownDirectives == nil {
		out.UnknownDirectives = []string{}
	}
	if len(e.ExtraDomains) > 0 {
		extras := append([]string{}, e.ExtraDomains...)
		out.ExtraDomains = &extras
	}
	setIf := func(dst **string, v string) {
		if v != "" {
			val := v
			*dst = &val
		}
	}
	setIf(&out.UpstreamScheme, e.UpstreamScheme)
	setIf(&out.UpstreamPort, e.UpstreamPort)
	setIf(&out.DnsProvider, e.DNSProvider)
	setIf(&out.DnsArgs, e.DNSArgs)
	setIf(&out.Encode, e.Encode)
	if len(e.HeaderUp) > 0 {
		headers := make([]CaddyHeaderUp, 0, len(e.HeaderUp))
		for _, h := range e.HeaderUp {
			headers = append(headers, CaddyHeaderUp{Name: h.Name, Value: h.Value})
		}
		out.HeaderUp = &headers
	}
	if e.InsecureUpstream {
		v := true
		out.InsecureUpstream = &v
	}
	return out
}

func toAPICaddyState(state *caddy.SitesState) CaddySitesState {
	out := CaddySitesState{
		Path:     state.Path,
		Writable: state.Writable,
		Entries:  []CaddySite{},
		RawSites: []CaddyRawSite{},
	}
	if state.Refusal != "" {
		r := state.Refusal
		out.Refusal = &r
	}
	if state.GlobalRaw != "" {
		g := state.GlobalRaw
		out.Global = &g
	}
	for _, e := range state.Entries {
		out.Entries = append(out.Entries, toAPICaddySite(e))
	}
	for _, r := range state.RawSites {
		out.RawSites = append(out.RawSites, CaddyRawSite{Addresses: r.Addresses, Reason: r.Reason, Raw: r.Raw})
	}
	if d := toAPISiteDefaults(caddy.InferSiteDefaults(state.Entries)); d != nil {
		out.SiteDefaults = d
	}
	return out
}

// toAPISiteDefaults maps inferred defaults; nil when there is nothing to inherit.
func toAPISiteDefaults(d caddy.SiteDefaults) *CaddySiteDefaults {
	if d.DNSProvider == "" && d.Encode == "" {
		return nil
	}
	out := &CaddySiteDefaults{}
	if d.DNSProvider != "" {
		out.DnsProvider = &d.DNSProvider
		if d.DNSArgs != "" {
			out.DnsArgs = &d.DNSArgs
		}
	}
	if d.Encode != "" {
		out.Encode = &d.Encode
	}
	return out
}

func siteFromInput(in CaddySiteInput) caddy.SiteEntry {
	site := caddy.SiteEntry{
		Domain:       in.Domain,
		UpstreamHost: in.UpstreamHost,
	}
	if in.ExtraDomains != nil {
		site.ExtraDomains = *in.ExtraDomains
	}
	get := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	site.UpstreamScheme = get(in.UpstreamScheme)
	site.UpstreamPort = get(in.UpstreamPort)
	site.DNSProvider = get(in.DnsProvider)
	site.DNSArgs = get(in.DnsArgs)
	site.Encode = get(in.Encode)
	if in.HeaderUp != nil {
		for _, h := range *in.HeaderUp {
			site.HeaderUp = append(site.HeaderUp, caddyfile.HeaderUp{Name: h.Name, Value: h.Value})
		}
	}
	if in.InsecureUpstream != nil {
		site.InsecureUpstream = *in.InsecureUpstream
	}
	return site
}

// ---- reads ------------------------------------------------------------------

func (s *Server) CaddySites(ctx context.Context, req CaddySitesRequestObject) (CaddySitesResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, _, err := s.caddyConn(ctx, id, req.Id)
	if err == store.ErrNotFound {
		return CaddySites404JSONResponse{notFoundErr("system")}, nil
	}
	if err != nil {
		return CaddySites502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	state, err := conn.Sites(ctx)
	if err != nil {
		return CaddySites502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	return CaddySites200JSONResponse(toAPICaddyState(state)), nil
}

func (s *Server) CaddyServed(ctx context.Context, req CaddyServedRequestObject) (CaddyServedResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, _, err := s.caddyConn(ctx, id, req.Id)
	if err == store.ErrNotFound {
		return CaddyServed404JSONResponse{notFoundErr("system")}, nil
	}
	if err != nil {
		return CaddyServed502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	res, err := conn.Served(ctx)
	if err != nil {
		return CaddyServed502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	out := CaddyServed{
		Upstreams: []struct {
			Dial   string  `json:"dial"`
			Domain string  `json:"domain"`
			Host   string  `json:"host"`
			Port   *string `json:"port,omitempty"`
		}{},
		Unresolvable: []struct {
			Domain string `json:"domain"`
			Reason string `json:"reason"`
		}{},
		DomainsWithoutProxy: res.DomainsWithoutProxy,
		Warnings:            res.Warnings,
	}
	for _, u := range res.Upstreams {
		item := struct {
			Dial   string  `json:"dial"`
			Domain string  `json:"domain"`
			Host   string  `json:"host"`
			Port   *string `json:"port,omitempty"`
		}{Dial: u.Dial, Domain: u.Domain, Host: u.Host}
		if u.Port != "" {
			p := u.Port
			item.Port = &p
		}
		out.Upstreams = append(out.Upstreams, item)
	}
	for _, u := range res.Unresolvable {
		out.Unresolvable = append(out.Unresolvable, struct {
			Domain string `json:"domain"`
			Reason string `json:"reason"`
		}{Domain: u.Domain, Reason: u.Reason})
	}
	return CaddyServed200JSONResponse(out), nil
}

// ---- mutations --------------------------------------------------------------

// caddyMutationOutcome funnels the three writes through one error mapping.
type caddyMutationOutcome struct {
	state   *caddy.SitesState
	code    int // 0=ok, else HTTP status class
	message string
}

func (s *Server) caddyMutate(ctx context.Context, instID uuid.UUID, action string,
	run func(conn *caddy.Conn, ctx context.Context) (*caddy.SitesState, error),
) caddyMutationOutcome {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return caddyMutationOutcome{code: 401}
	}
	conn, resource, err := s.caddyConn(ctx, id, instID)
	if err == store.ErrNotFound {
		return caddyMutationOutcome{code: 404}
	}
	if err != nil {
		return caddyMutationOutcome{code: 502, message: err.Error()}
	}
	// Adding, re-pointing or removing a site rewrites the public reverse
	// proxy's configuration — configure, which the Operator role lacks.
	if !s.can(ctx, id, resource, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, action, resource)
		return caddyMutationOutcome{code: 403}
	}
	mctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	state, err := run(conn, mctx)
	if err != nil {
		s.audit.Log(ctx, actorFor(id), action, resource, "error", map[string]any{"error": err.Error()})
		switch {
		case caddy.IsConflict(err):
			return caddyMutationOutcome{code: 409, message: err.Error()}
		case caddy.IsRejected(err), caddy.IsReadOnly(err):
			return caddyMutationOutcome{code: 400, message: err.Error()}
		default:
			// Validation errors read like instructions; upstream failures
			// name the broken machine. Both are 400-class for the caller.
			return caddyMutationOutcome{code: 400, message: err.Error()}
		}
	}
	s.audit.Log(ctx, actorFor(id), action, resource, "success", nil)
	return caddyMutationOutcome{state: state}
}

func (s *Server) CaddyCreateSite(ctx context.Context, req CaddyCreateSiteRequestObject) (CaddyCreateSiteResponseObject, error) {
	site := siteFromInput(*req.Body)
	out := s.caddyMutate(ctx, req.Id, "caddy.site_create", func(conn *caddy.Conn, ctx context.Context) (*caddy.SitesState, error) {
		return conn.CreateSite(ctx, site)
	})
	switch out.code {
	case 0:
		return CaddyCreateSite200JSONResponse(toAPICaddyState(out.state)), nil
	case 401:
		return nil, ErrUnauthenticated
	case 403:
		return CaddyCreateSite403JSONResponse{forbidden()}, nil
	case 404:
		return CaddyCreateSite404JSONResponse{notFoundErr("system")}, nil
	case 409:
		return CaddyCreateSite409JSONResponse{ConflictJSONResponse(errBody("conflict", out.message))}, nil
	case 502:
		return CaddyCreateSite502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", out.message))}, nil
	default:
		return CaddyCreateSite400JSONResponse{BadRequestJSONResponse(errBody("rejected", out.message))}, nil
	}
}

func (s *Server) CaddyUpdateSite(ctx context.Context, req CaddyUpdateSiteRequestObject) (CaddyUpdateSiteResponseObject, error) {
	site := siteFromInput(*req.Body)
	out := s.caddyMutate(ctx, req.Id, "caddy.site_update", func(conn *caddy.Conn, ctx context.Context) (*caddy.SitesState, error) {
		return conn.UpdateSite(ctx, req.Domain, site)
	})
	switch out.code {
	case 0:
		return CaddyUpdateSite200JSONResponse(toAPICaddyState(out.state)), nil
	case 401:
		return nil, ErrUnauthenticated
	case 403:
		return CaddyUpdateSite403JSONResponse{forbidden()}, nil
	case 404:
		return CaddyUpdateSite404JSONResponse{notFoundErr("system")}, nil
	case 409:
		return CaddyUpdateSite409JSONResponse{ConflictJSONResponse(errBody("conflict", out.message))}, nil
	case 502:
		return CaddyUpdateSite502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", out.message))}, nil
	default:
		return CaddyUpdateSite400JSONResponse{BadRequestJSONResponse(errBody("rejected", out.message))}, nil
	}
}

func (s *Server) CaddyRemoveSite(ctx context.Context, req CaddyRemoveSiteRequestObject) (CaddyRemoveSiteResponseObject, error) {
	out := s.caddyMutate(ctx, req.Id, "caddy.site_remove", func(conn *caddy.Conn, ctx context.Context) (*caddy.SitesState, error) {
		return conn.RemoveSite(ctx, req.Domain)
	})
	switch out.code {
	case 0:
		return CaddyRemoveSite200JSONResponse(toAPICaddyState(out.state)), nil
	case 401:
		return nil, ErrUnauthenticated
	case 403:
		return CaddyRemoveSite403JSONResponse{forbidden()}, nil
	case 404:
		return CaddyRemoveSite404JSONResponse{notFoundErr("system")}, nil
	case 409:
		return CaddyRemoveSite409JSONResponse{ConflictJSONResponse(errBody("conflict", out.message))}, nil
	case 502:
		return CaddyRemoveSite502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", out.message))}, nil
	default:
		return CaddyRemoveSite400JSONResponse{BadRequestJSONResponse(errBody("rejected", out.message))}, nil
	}
}
