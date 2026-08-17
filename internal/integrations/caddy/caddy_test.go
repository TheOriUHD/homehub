package caddy

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"homehub/internal/caddysim"
	"homehub/internal/integration"
)

func newTestConn(t *testing.T) (*Conn, *caddysim.Sim) {
	t.Helper()
	sim := caddysim.New()
	srv := httptest.NewServer(sim)
	t.Cleanup(srv.Close)

	code := sim.OpenPairing()
	token, err := Pair(context.Background(), srv.URL, code)
	if err != nil {
		t.Fatalf("Pair: %v", err)
	}
	c, err := newClient(map[string]any{"baseUrl": srv.URL}, map[string]string{"token": token})
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	return &Conn{
		inst:   integration.Instance{ID: uuid.New(), Type: TypeID, Name: "sim"},
		client: c,
		deps:   integration.Deps{Publish: func(string, any) {}},
	}, sim
}

func TestPairingSemantics(t *testing.T) {
	sim := caddysim.New()
	srv := httptest.NewServer(sim)
	defer srv.Close()
	ctx := context.Background()

	// No window open.
	if _, err := Pair(ctx, srv.URL, "123456"); err == nil || !strings.Contains(err.Error(), "no pairing window") {
		t.Fatalf("expected no-window error, got %v", err)
	}

	// Wrong guesses burn the window at five.
	sim.OpenPairing()
	var lastErr error
	for i := 0; i < 5; i++ {
		_, lastErr = Pair(ctx, srv.URL, "000000")
	}
	if lastErr == nil || !strings.Contains(lastErr.Error(), "window is closed") {
		t.Fatalf("fifth wrong guess must burn the window, got %v", lastErr)
	}

	// A fresh window pairs; the code is single use.
	code := sim.OpenPairing()
	token, err := Pair(ctx, srv.URL, code)
	if err != nil || len(token) != 64 {
		t.Fatalf("pair: %v (token %q)", err, token)
	}
	if _, err := Pair(ctx, srv.URL, code); err == nil {
		t.Fatal("a code must be worth nothing a second time")
	}

	// Expiry.
	sim.OpenPairing()
	sim.ExpirePairing()
	if _, err := Pair(ctx, srv.URL, "111111"); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expected expiry error, got %v", err)
	}
}

func TestAdminAPIGuard(t *testing.T) {
	if _, err := normalizeBaseURL("10.0.0.2:2019"); err == nil || !strings.Contains(err.Error(), "admin API") {
		t.Fatalf("port 2019 must be refused: %v", err)
	}
	if _, err := normalizeBaseURL("10.0.0.2:9776"); err != nil {
		t.Fatalf("normal helper port refused: %v", err)
	}
}

func TestSitesRead(t *testing.T) {
	conn, _ := newTestConn(t)
	sites, err := conn.Sites(context.Background())
	if err != nil {
		t.Fatalf("Sites: %v", err)
	}
	if sites.Refusal != "" || !sites.Writable {
		t.Fatalf("state: %+v", sites)
	}
	if len(sites.Entries) != 2 || len(sites.RawSites) != 1 {
		t.Fatalf("expected 2 entries + 1 raw, got %d + %d", len(sites.Entries), len(sites.RawSites))
	}
	app := sites.Entries[0]
	if app.Domain != "app.example.com" || app.UpstreamHost != "10.0.0.5" || app.UpstreamPort != "8443" {
		t.Fatalf("app entry: %+v", app)
	}
	if app.DNSProvider != "cloudflare" || app.DNSArgs != "{env.CLOUDFLARE_API_TOKEN}" {
		t.Fatalf("dns challenge: %+v", app)
	}
	if sites.RawSites[0].Reason != "no reverse_proxy" {
		t.Fatalf("raw reason: %q", sites.RawSites[0].Reason)
	}
	if !strings.Contains(sites.GlobalRaw, "email admin@example.com") {
		t.Fatalf("global block missing: %q", sites.GlobalRaw)
	}
}

func TestMutationsRoundTrip(t *testing.T) {
	conn, sim := newTestConn(t)
	ctx := context.Background()

	// create
	state, err := conn.CreateSite(ctx, SiteEntry{
		Domain: "New.Example.COM", UpstreamHost: "10.0.0.9", UpstreamPort: "3000",
	})
	if err != nil {
		t.Fatalf("CreateSite: %v", err)
	}
	if len(state.Entries) != 3 {
		t.Fatalf("expected 3 entries after create, got %d", len(state.Entries))
	}
	if !strings.Contains(sim.Caddyfile(), "new.example.com {") {
		t.Fatal("create did not reach the file (or domain was not normalized)")
	}
	// comments survived
	if !strings.Contains(sim.Caddyfile(), "# static assets") {
		t.Fatal("a comment was lost by the write")
	}

	// duplicate create refused before any write
	if _, err := conn.CreateSite(ctx, SiteEntry{Domain: "app.example.com", UpstreamHost: "x"}); err == nil {
		t.Fatal("duplicate domain must refuse")
	}

	// update
	if _, err := conn.UpdateSite(ctx, "grafana.example.com", SiteEntry{
		Domain: "grafana.example.com", UpstreamHost: "10.0.0.6", UpstreamPort: "3001",
	}); err != nil {
		t.Fatalf("UpdateSite: %v", err)
	}
	if !strings.Contains(sim.Caddyfile(), "10.0.0.6:3001") {
		t.Fatal("update did not change the port")
	}

	// remove
	state, err = conn.RemoveSite(ctx, "new.example.com")
	if err != nil {
		t.Fatalf("RemoveSite: %v", err)
	}
	if strings.Contains(sim.Caddyfile(), "new.example.com") || len(state.Entries) != 2 {
		t.Fatal("remove left the site behind")
	}

	// validation: URL pasted into the domain field
	if _, err := conn.CreateSite(ctx, SiteEntry{Domain: "https://x.example.com", UpstreamHost: "y"}); err == nil ||
		!strings.Contains(err.Error(), "without the scheme") {
		t.Fatalf("scheme in domain: %v", err)
	}
}

func TestReadOnlyAndRejection(t *testing.T) {
	conn, sim := newTestConn(t)
	ctx := context.Background()

	sim.SetWritable(false)
	_, err := conn.CreateSite(ctx, SiteEntry{Domain: "x.example.com", UpstreamHost: "h"})
	if !IsReadOnly(err) {
		t.Fatalf("read-only helper must yield the 403 mapping, got %v", err)
	}
	sim.SetWritable(true)

	// Caddy rejects → 400, nothing on disk touched.
	before := sim.Caddyfile()
	_, err = conn.CreateSite(ctx, SiteEntry{
		Domain: "bad.example.com", UpstreamHost: "h", Encode: "gzip",
		// The sim's validator rejects this marker like Caddy would an
		// unknown directive.
		DNSProvider: "not_a_real_directive",
	})
	if !IsRejected(err) {
		t.Fatalf("expected the 400 rejection mapping, got %v", err)
	}
	if sim.Caddyfile() != before {
		t.Fatal("a rejected config must be a non-event on disk")
	}
}

func TestConflictOnConcurrentEdit(t *testing.T) {
	conn, sim := newTestConn(t)
	ctx := context.Background()

	// Simulate a hand edit between our read and write by wrapping mutate:
	// read state now, then have the "other tab" (direct sim mutation) win.
	state, err := fetchCaddyfile(ctx, conn.client)
	if err != nil {
		t.Fatal(err)
	}
	sim.SetCaddyfile(state.Text + "\n# hand edit\n")

	doc := struct{ text, sha string }{state.Text, state.Sha256}
	err = conn.client.do(ctx, "PUT", "/caddyfile", map[string]string{"text": doc.text, "sha256": doc.sha}, nil)
	if !IsConflict(err) {
		t.Fatalf("expected 409 conflict, got %v", err)
	}
	if !strings.Contains(err.Error(), "Reload and reapply") {
		t.Fatalf("conflict message should explain what to do: %v", err)
	}
}

func TestServedProjection(t *testing.T) {
	conn, _ := newTestConn(t)
	res, err := conn.Served(context.Background())
	if err != nil {
		t.Fatalf("Served: %v", err)
	}
	byDomain := map[string]string{}
	for _, u := range res.Upstreams {
		byDomain[u.Domain] = u.Dial
	}
	if byDomain["app.example.com"] != "10.0.0.5:8443" || byDomain["grafana.example.com"] != "10.0.0.6:3000" {
		t.Fatalf("upstreams: %+v", res.Upstreams)
	}
	found := false
	for _, d := range res.DomainsWithoutProxy {
		if d == "files.example.com" {
			found = true
		}
	}
	if !found {
		t.Fatalf("files.example.com should be served-without-proxy: %+v", res.DomainsWithoutProxy)
	}
}

func TestHealthStates(t *testing.T) {
	conn, sim := newTestConn(t)
	ctx := context.Background()

	if h := conn.Health(ctx); h.Status != integration.StatusConnected {
		t.Fatalf("healthy: %+v", h)
	}
	sim.SetCaddyDown(true)
	if h := conn.Health(ctx); h.Status != integration.StatusDegraded {
		t.Fatalf("caddy down should be degraded (file still readable): %+v", h)
	}
}

func TestRefusalBlocksMutations(t *testing.T) {
	conn, sim := newTestConn(t)
	sim.SetCaddyfile("shorthand.example.com\nreverse_proxy :8080\n")
	sites, err := conn.Sites(context.Background())
	if err != nil {
		t.Fatalf("Sites: %v", err)
	}
	if sites.Refusal == "" || len(sites.Entries) != 0 {
		t.Fatalf("shorthand file must refuse wholesale: %+v", sites)
	}
	if _, err := conn.CreateSite(context.Background(), SiteEntry{Domain: "x.example.com", UpstreamHost: "h"}); err == nil {
		t.Fatal("mutations must refuse while the file is refused")
	}
}
