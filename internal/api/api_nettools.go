package api

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"homehub/internal/httpctx"
	"homehub/internal/nettools"
	"homehub/internal/store"
)

// Network utility handlers (the server-backed tools). All make OUTBOUND
// connections on behalf of the signed-in user; each is auth-gated (identity
// required) and bounded inside the nettools package. No RBAC resource —
// consistent with the personal utilities area.

// wellKnownResolvers maps a resolver id to its DNS server (host:port).
var wellKnownResolvers = map[string]string{
	"system":     "",
	"cloudflare": "1.1.1.1:53",
	"google":     "8.8.8.8:53",
	"quad9":      "9.9.9.9:53",
}

func (s *Server) ToolSSL(ctx context.Context, req ToolSSLRequestObject) (ToolSSLResponseObject, error) {
	if httpctx.IdentityFrom(ctx) == nil {
		return nil, ErrUnauthenticated
	}
	host := strings.TrimSpace(req.Body.Host)
	host = strings.TrimPrefix(strings.TrimPrefix(host, "https://"), "http://")
	host = strings.TrimSuffix(host, "/")
	if host == "" {
		return ToolSSL400JSONResponse{BadRequestJSONResponse(errBody("bad_host", "enter a host name"))}, nil
	}
	port := 443
	if req.Body.Port != nil && *req.Body.Port > 0 {
		port = *req.Body.Port
	}
	r, err := nettools.InspectTLS(ctx, host, port, time.Now())
	if err != nil {
		return ToolSSL400JSONResponse{BadRequestJSONResponse(errBody("tls_failed", err.Error()))}, nil
	}
	return ToolSSL200JSONResponse(toSSLResponse(r)), nil
}

func (s *Server) ToolDNSResolvers(ctx context.Context, _ ToolDNSResolversRequestObject) (ToolDNSResolversResponseObject, error) {
	if httpctx.IdentityFrom(ctx) == nil {
		return nil, ErrUnauthenticated
	}
	out := ToolDNSResolvers200JSONResponse{
		{Id: "system", Label: "System default"},
		{Id: "cloudflare", Label: "Cloudflare (1.1.1.1)", Server: ptr("1.1.1.1:53")},
		{Id: "google", Label: "Google (8.8.8.8)", Server: ptr("8.8.8.8:53")},
		{Id: "quad9", Label: "Quad9 (9.9.9.9)", Server: ptr("9.9.9.9:53")},
	}
	if host := s.adguardHost(ctx); host != "" {
		out = append(out, ResolverOption{Id: "adguard", Label: "Your DNS filter (" + host + ")", Server: ptr(net.JoinHostPort(host, "53"))})
	}
	out = append(out, ResolverOption{Id: "custom", Label: "Custom server…"})
	return out, nil
}

// adguardHost returns the host of a configured adguard/pihole integration.
func (s *Server) adguardHost(ctx context.Context) string {
	rows, err := s.store.ListIntegrations(ctx)
	if err != nil {
		return ""
	}
	for _, row := range rows {
		if row.Type == "adguard" || row.Type == "pihole" {
			var cfg struct {
				Host string `json:"host"`
			}
			if len(row.Config) > 0 && json.Unmarshal(row.Config, &cfg) == nil && cfg.Host != "" {
				return cfg.Host
			}
		}
	}
	return ""
}

func (s *Server) ToolDNS(ctx context.Context, req ToolDNSRequestObject) (ToolDNSResponseObject, error) {
	if httpctx.IdentityFrom(ctx) == nil {
		return nil, ErrUnauthenticated
	}
	name := strings.TrimSpace(req.Body.Name)
	if name == "" {
		return ToolDNS400JSONResponse{BadRequestJSONResponse(errBody("bad_name", "enter a name to look up"))}, nil
	}
	resolver := "system"
	if req.Body.Resolver != nil {
		resolver = *req.Body.Resolver
	}
	server := ""
	switch resolver {
	case "custom":
		if req.Body.CustomServer == nil || strings.TrimSpace(*req.Body.CustomServer) == "" {
			return ToolDNS400JSONResponse{BadRequestJSONResponse(errBody("bad_server", "enter a custom resolver address"))}, nil
		}
		server = withDNSPort(strings.TrimSpace(*req.Body.CustomServer))
	case "adguard":
		if host := s.adguardHost(ctx); host != "" {
			server = net.JoinHostPort(host, "53")
		}
	default:
		server = wellKnownResolvers[resolver]
	}
	res := nettools.Lookup(ctx, name, string(req.Body.Type), server)
	res.Resolver = resolver
	return ToolDNS200JSONResponse(toDNSResponse(res)), nil
}

func (s *Server) ToolReachability(ctx context.Context, req ToolReachabilityRequestObject) (ToolReachabilityResponseObject, error) {
	if httpctx.IdentityFrom(ctx) == nil {
		return nil, ErrUnauthenticated
	}
	host := strings.TrimSpace(req.Body.Host)
	if host == "" {
		return ToolReachability400JSONResponse{BadRequestJSONResponse(errBody("bad_host", "enter a host"))}, nil
	}
	rctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	out := ReachResult{Host: host}
	if ip := nettools.ResolveHost(rctx, host); ip != "" {
		out.Ip = &ip
	}
	if req.Body.Port != nil && *req.Body.Port > 0 {
		t := nettools.CheckTCP(rctx, host, *req.Body.Port)
		out.Tcp = &ReachTCP{LatencyMs: float32(t.LatencyMs), Open: t.Open, Port: t.Port}
		if t.Error != "" {
			out.Tcp.Error = &t.Error
		}
	}
	p := nettools.Ping(rctx, host)
	out.Ping = &ReachPing{Alive: p.Alive, Available: p.Available}
	if p.RttMs > 0 {
		r := float32(p.RttMs)
		out.Ping.RttMs = &r
	}
	if p.Error != "" {
		out.Ping.Error = &p.Error
	}
	return ToolReachability200JSONResponse(out), nil
}

func (s *Server) ToolHTTP(ctx context.Context, req ToolHTTPRequestObject) (ToolHTTPResponseObject, error) {
	if httpctx.IdentityFrom(ctx) == nil {
		return nil, ErrUnauthenticated
	}
	headers := map[string]string{}
	if req.Body.Headers != nil {
		headers = *req.Body.Headers
	}
	body := ""
	if req.Body.Body != nil {
		body = *req.Body.Body
	}
	r, err := nettools.DoHTTP(ctx, nettools.HTTPRequest{
		Method: req.Body.Method, URL: req.Body.Url, Headers: headers, Body: body,
	})
	if err != nil {
		return ToolHTTP400JSONResponse{BadRequestJSONResponse(errBody("http_failed", err.Error()))}, nil
	}
	return ToolHTTP200JSONResponse(toHTTPResponse(r)), nil
}

// ---- Wake-on-LAN ------------------------------------------------------------

func (s *Server) ListWolTargets(ctx context.Context, _ ListWolTargetsRequestObject) (ListWolTargetsResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	rows, err := s.store.ListWolTargets(ctx, id.User.ID)
	if err != nil {
		return nil, err
	}
	out := make(ListWolTargets200JSONResponse, 0, len(rows))
	for i := range rows {
		out = append(out, toWolTarget(&rows[i]))
	}
	return out, nil
}

func (s *Server) CreateWolTarget(ctx context.Context, req CreateWolTargetRequestObject) (CreateWolTargetResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	name := strings.TrimSpace(req.Body.Name)
	mac := normalizeMAC(req.Body.Mac)
	if name == "" || mac == "" {
		return CreateWolTarget400JSONResponse{BadRequestJSONResponse(errBody("bad_input", "a name and a valid MAC address are required"))}, nil
	}
	broadcast := "255.255.255.255"
	if req.Body.Broadcast != nil && strings.TrimSpace(*req.Body.Broadcast) != "" {
		if net.ParseIP(strings.TrimSpace(*req.Body.Broadcast)) == nil {
			return CreateWolTarget400JSONResponse{BadRequestJSONResponse(errBody("bad_broadcast", "the broadcast address is not a valid IP"))}, nil
		}
		broadcast = strings.TrimSpace(*req.Body.Broadcast)
	}
	port := 9
	if req.Body.Port != nil && *req.Body.Port > 0 && *req.Body.Port < 65536 {
		port = *req.Body.Port
	}
	t, err := s.store.CreateWolTarget(ctx, id.User.ID, name, mac, broadcast, port)
	if err != nil {
		return nil, err
	}
	return CreateWolTarget200JSONResponse(toWolTarget(t)), nil
}

func (s *Server) DeleteWolTarget(ctx context.Context, req DeleteWolTargetRequestObject) (DeleteWolTargetResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	gone, err := s.store.DeleteWolTarget(ctx, id.User.ID, req.Id)
	if err != nil {
		return nil, err
	}
	if !gone {
		return DeleteWolTarget404JSONResponse{NotFoundJSONResponse(errBody("not_found", "no such target"))}, nil
	}
	return DeleteWolTarget204Response{}, nil
}

func (s *Server) WolWake(ctx context.Context, req WolWakeRequestObject) (WolWakeResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	mac, broadcast, port := "", "255.255.255.255", 9
	if req.Body.TargetId != nil {
		t, err := s.store.GetWolTarget(ctx, id.User.ID, *req.Body.TargetId)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return WolWake404JSONResponse{NotFoundJSONResponse(errBody("not_found", "no such target"))}, nil
			}
			return nil, err
		}
		mac, broadcast, port = t.MAC, t.Broadcast, t.Port
	} else {
		if req.Body.Mac == nil {
			return WolWake400JSONResponse{BadRequestJSONResponse(errBody("bad_input", "provide a saved targetId or a MAC address"))}, nil
		}
		mac = normalizeMAC(*req.Body.Mac)
		if req.Body.Broadcast != nil && strings.TrimSpace(*req.Body.Broadcast) != "" {
			broadcast = strings.TrimSpace(*req.Body.Broadcast)
		}
		if req.Body.Port != nil && *req.Body.Port > 0 {
			port = *req.Body.Port
		}
	}
	if mac == "" {
		return WolWake400JSONResponse{BadRequestJSONResponse(errBody("bad_mac", "not a valid MAC address"))}, nil
	}
	if err := nettools.SendMagicPacket(mac, broadcast, port); err != nil {
		return WolWake400JSONResponse{BadRequestJSONResponse(errBody("wol_failed", err.Error()))}, nil
	}
	s.audit.Log(ctx, actorFor(id), "tools.wol.wake", "tools/wol", "ok", map[string]any{"mac": mac})
	return WolWake200JSONResponse{Sent: true, Mac: ptr(mac)}, nil
}

func normalizeMAC(raw string) string {
	hw, err := net.ParseMAC(strings.TrimSpace(raw))
	if err != nil || len(hw) != 6 {
		return ""
	}
	return hw.String()
}

func withDNSPort(server string) string {
	if _, _, err := net.SplitHostPort(server); err == nil {
		return server
	}
	return net.JoinHostPort(server, "53")
}

func ptr[T any](v T) *T { return &v }

// ---- mappers (nettools structs -> generated API types) ----------------------

func toSSLResponse(r *nettools.SSLResult) SSLResult {
	out := SSLResult{
		Host: r.Host, Port: r.Port, Valid: r.Valid, Expired: r.Expired,
		DaysRemaining: r.DaysRemaining, Leaf: toCertInfo(r.Leaf),
		TlsVersion: r.TLSVersion, Cipher: r.Cipher, Chain: []CertInfo{},
	}
	if r.ValidationErr != "" {
		out.ValidationError = &r.ValidationErr
	}
	for _, c := range r.Chain {
		out.Chain = append(out.Chain, toCertInfo(c))
	}
	return out
}

func toCertInfo(c nettools.CertInfo) CertInfo {
	ci := CertInfo{
		Subject: c.Subject, Issuer: c.Issuer, NotBefore: c.NotBefore, NotAfter: c.NotAfter,
		Serial: c.Serial, SigAlgo: c.SigAlgo, IsCA: c.IsCA,
	}
	if len(c.DNSNames) > 0 {
		names := c.DNSNames
		ci.DnsNames = &names
	}
	return ci
}

func toDNSResponse(r nettools.DNSResult) DNSResult {
	out := DNSResult{
		Name: r.Name, Type: r.Type, Resolver: r.Resolver, QueryMs: float32(r.QueryMs),
		Records: []DNSRecord{},
	}
	if r.Server != "" {
		out.Server = &r.Server
	}
	if r.Error != "" {
		out.Error = &r.Error
	}
	for _, rec := range r.Records {
		out.Records = append(out.Records, DNSRecord{Type: rec.Type, Value: rec.Value})
	}
	return out
}

func toHTTPResponse(r *nettools.HTTPResponse) HTTPResult {
	return HTTPResult{
		Status: r.Status, StatusText: r.StatusText, Proto: r.Proto,
		DurationMs: float32(r.DurationMs), Size: r.Size, Headers: r.Headers,
		HeaderKeys: r.HeaderKeys, Body: r.Body, Truncated: r.Truncated,
		Binary: r.Binary, FinalUrl: r.FinalURL,
	}
}

func toWolTarget(t *store.WolTarget) WolTarget {
	return WolTarget{
		Id: t.ID, Name: t.Name, Mac: t.MAC, Broadcast: t.Broadcast,
		Port: t.Port, CreatedAt: t.CreatedAt,
	}
}
