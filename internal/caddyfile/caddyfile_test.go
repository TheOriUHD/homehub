package caddyfile

import (
	"strings"
	"testing"
)

// realistic fixture: global options, DNS challenge with env placeholder,
// header_up, comments between blocks, unusual whitespace, an advanced block,
// and a redir carrying {uri} mid-line.
const fixture = `{
	email admin@example.com
	admin 127.0.0.1:2019
}

# --- main site ---
app.example.com, www.app.example.com {
	tls {
		dns cloudflare {env.CLOUDFLARE_API_TOKEN}
	}

	encode gzip zstd
	reverse_proxy https://10.0.0.5:8443 {
		transport http {
			tls_insecure_skip_verify
		}
		header_up Host {host}
	}
}


nas.example.com {
	reverse_proxy [fd00::1]:5000
}

# static files, not a proxy
files.example.com {
	root * /srv/files
	file_server browse
}

old.example.com {
	redir https://app.example.com{uri} permanent
	reverse_proxy 127.0.0.1:9999
}

multi.example.com {
	reverse_proxy /api/* 10.0.0.7:8080
}
`

func TestRoundTripByteExact(t *testing.T) {
	doc := Split(fixture)
	if doc.Refusal != "" {
		t.Fatalf("unexpected refusal: %s", doc.Refusal)
	}
	if got := Render(doc); got != fixture {
		t.Fatalf("round trip is not byte-identical.\n--- got ---\n%q\n--- want ---\n%q", got, fixture)
	}
}

func TestClassification(t *testing.T) {
	doc := Split(fixture)
	var kinds []BlockKind
	for _, b := range doc.Blocks {
		kinds = append(kinds, b.Kind)
	}
	// global, gap(comment), entry(app), gap, entry(nas), gap(comment),
	// raw(files), gap, entry(old — redir is a bare line!), gap, raw(multi), …
	if doc.Blocks[0].Kind != KindGlobal {
		t.Fatalf("first block should be global, got %v (%v)", doc.Blocks[0].Kind, kinds)
	}

	app := doc.Blocks[FindEntry(doc, "app.example.com")]
	e := app.Entry
	if len(e.Domains) != 2 || e.Domains[1] != "www.app.example.com" {
		t.Fatalf("domains: %+v", e.Domains)
	}
	if e.Upstream.Scheme != "https" || e.Upstream.Host != "10.0.0.5" || e.Upstream.Port != "8443" {
		t.Fatalf("upstream: %+v", e.Upstream)
	}
	if e.DNSChallenge == nil || e.DNSChallenge.Provider != "cloudflare" || e.DNSChallenge.Args != "{env.CLOUDFLARE_API_TOKEN}" {
		t.Fatalf("dns challenge: %+v", e.DNSChallenge)
	}
	if e.Encode != "gzip zstd" || !e.InsecureUpstream {
		t.Fatalf("options: encode=%q insecure=%v", e.Encode, e.InsecureUpstream)
	}
	if len(e.HeaderUp) != 1 || e.HeaderUp[0] != (HeaderUp{Name: "Host", Value: "{host}"}) {
		t.Fatalf("header_up: %+v", e.HeaderUp)
	}

	nas := doc.Blocks[FindEntry(doc, "nas.example.com")].Entry
	if nas.Upstream.Host != "fd00::1" || nas.Upstream.Port != "5000" {
		t.Fatalf("ipv6 upstream: %+v", nas.Upstream)
	}

	// The redir with {uri} mid-line must stay a bare unknown directive, and
	// the site must still be an editable entry.
	old := doc.Blocks[FindEntry(doc, "old.example.com")].Entry
	if len(old.UnknownDirectives) != 1 || !strings.Contains(old.UnknownDirectives[0], "{uri}") {
		t.Fatalf("redir with {uri} should be an unknown directive: %+v", old.UnknownDirectives)
	}

	// files: no reverse_proxy → raw with the reason.
	var files *Block
	for i := range doc.Blocks {
		for _, a := range doc.Blocks[i].Addresses {
			if a == "files.example.com" {
				files = &doc.Blocks[i]
			}
		}
	}
	if files == nil || files.Kind != KindRaw || files.Reason != "no reverse_proxy" {
		t.Fatalf("files block: %+v", files)
	}

	// multi: matcher on reverse_proxy → raw.
	var multi *Block
	for i := range doc.Blocks {
		for _, a := range doc.Blocks[i].Addresses {
			if a == "multi.example.com" {
				multi = &doc.Blocks[i]
			}
		}
	}
	if multi == nil || multi.Kind != KindRaw || multi.Reason != "reverse_proxy has matchers or several upstreams" {
		t.Fatalf("multi block: %+v", multi)
	}
}

func TestRefusals(t *testing.T) {
	if doc := Split("example.com\nreverse_proxy :8080\n"); doc.Refusal != RefusalShorthand {
		t.Fatalf("shorthand should refuse, got %q", doc.Refusal)
	}
	if doc := Split("a.example.com {\n\treverse_proxy :8080\n"); doc.Refusal != RefusalBraces {
		t.Fatalf("unbalanced should refuse, got %q", doc.Refusal)
	}
	if doc := Split("a.example.com {\n\treverse_proxy :8080\n}\n"); doc.Refusal == "" {
		// sanity: balanced is fine — port-only upstreams have no host, so
		// this block is raw, but the file is manageable.
		_ = doc
	} else {
		t.Fatalf("balanced file refused: %q", doc.Refusal)
	}
}

func TestEntryRenderRoundTrip(t *testing.T) {
	entry := Entry{
		Domains:  []string{"svc.example.com"},
		Upstream: Upstream{Scheme: "https", Host: "10.0.0.9", Port: "8443"},
		DNSChallenge: &DNSChallenge{
			Provider: "cloudflare", Args: "{env.CLOUDFLARE_API_TOKEN}",
		},
		Encode:            "gzip",
		HeaderUp:          []HeaderUp{{Name: "X-Real-IP", Value: "{remote_host}"}},
		InsecureUpstream:  true,
		UnknownDirectives: []string{"log"},
	}
	raw := RenderEntry(entry)
	doc := Split(raw)
	if doc.Refusal != "" {
		t.Fatalf("rendered entry refused: %s", doc.Refusal)
	}
	if len(doc.Blocks) != 1 || doc.Blocks[0].Kind != KindEntry {
		t.Fatalf("rendered entry did not re-recognise: %+v", doc.Blocks)
	}
	got := doc.Blocks[0].Entry
	if got.Upstream != entry.Upstream || got.Encode != entry.Encode ||
		got.InsecureUpstream != entry.InsecureUpstream ||
		*got.DNSChallenge != *entry.DNSChallenge ||
		len(got.HeaderUp) != 1 || got.HeaderUp[0] != entry.HeaderUp[0] ||
		len(got.UnknownDirectives) != 1 || got.UnknownDirectives[0] != "log" {
		t.Fatalf("entry did not survive render→split: %+v", got)
	}
	// And the render is stable: split→render gives the same bytes back.
	if Render(doc) != raw {
		t.Fatal("rendered entry is not byte-stable")
	}
}

func TestMutations(t *testing.T) {
	doc := Split(fixture)

	// create: duplicate refused
	if _, err := Create(doc, Entry{Domains: []string{"app.example.com"}, Upstream: Upstream{Host: "x", Port: "1"}}); err == nil {
		t.Fatal("duplicate create must refuse")
	}
	// create: appended and re-splittable, others untouched
	doc2, err := Create(doc, Entry{Domains: []string{"new.example.com"}, Upstream: Upstream{Host: "10.0.0.8", Port: "3000"}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	text2 := Render(doc2)
	if !strings.Contains(text2, "new.example.com {") || !strings.HasPrefix(text2, fixture[:len(fixture)-1]) {
		t.Fatalf("create must append without touching the rest:\n%s", text2)
	}
	if re := Split(text2); re.Refusal != "" || FindEntry(re, "new.example.com") < 0 {
		t.Fatal("created file did not re-split")
	}

	// update: carries unknown directives across
	doc3, err := Update(doc, "old.example.com", Entry{
		Domains:  []string{"old.example.com"},
		Upstream: Upstream{Host: "127.0.0.1", Port: "8888"},
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	updated := doc3.Blocks[FindEntry(doc3, "old.example.com")]
	if !strings.Contains(updated.Raw, "{uri}") {
		t.Fatal("update dropped the unknown redir directive")
	}
	if !strings.Contains(updated.Raw, ":8888") {
		t.Fatal("update did not change the port")
	}
	// update: rename onto an existing domain refused
	if _, err := Update(doc, "old.example.com", Entry{Domains: []string{"nas.example.com"}, Upstream: Upstream{Host: "x"}}); err == nil {
		t.Fatal("rename onto existing domain must refuse")
	}

	// remove: entry gone, comment gaps preserved
	doc4, err := Remove(doc, "nas.example.com")
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	text4 := Render(doc4)
	if strings.Contains(text4, "nas.example.com") {
		t.Fatal("remove left the entry behind")
	}
	if !strings.Contains(text4, "# static files, not a proxy") {
		t.Fatal("remove ate a comment gap")
	}
}

func TestValidation(t *testing.T) {
	if _, err := NormalizeDomain("https://x.example.com"); err == nil || !strings.Contains(err.Error(), "without the scheme") {
		t.Fatalf("scheme: %v", err)
	}
	if _, err := NormalizeDomain("x.example.com/path"); err == nil {
		t.Fatal("path must be rejected")
	}
	if d, err := NormalizeDomain("X.Example.COM."); err != nil || d != "x.example.com" {
		t.Fatalf("normalize: %q %v", d, err)
	}
	if d, err := NormalizeDomain("*.example.com"); err != nil || d != "*.example.com" {
		t.Fatalf("wildcard: %q %v", d, err)
	}
	if err := ValidateUpstreamHost("10.0.0.5/api"); err == nil {
		t.Fatal("slash in host must be rejected")
	}
	if err := ValidatePort("70000"); err == nil {
		t.Fatal("port range must be enforced")
	}
	if err := ValidatePort(""); err != nil {
		t.Fatal("empty port is allowed")
	}
}

func TestHeadersAreDefaults(t *testing.T) {
	if !HeadersAreDefaults([]HeaderUp{{Name: "Host", Value: "{host}"}}) {
		t.Fatal("restated default should be detected")
	}
	if HeadersAreDefaults([]HeaderUp{{Name: "X-Api-Key", Value: "abc"}}) {
		t.Fatal("custom header is not a default")
	}
}
