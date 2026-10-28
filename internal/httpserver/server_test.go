package httpserver

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"homehub/internal/api"
)

// readAll is a handler that reports whether it could drain the whole body.
var readAll = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	if _, err := io.Copy(io.Discard, r.Body); err != nil {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		return
	}
	w.WriteHeader(http.StatusOK)
})

func TestJSONBodyLimit_ConverterUploadGetsItsOwnCap(t *testing.T) {
	h := jsonBodyLimit(readAll)
	big := bytes.Repeat([]byte("x"), maxJSONBody+1<<20) // just over the JSON cap

	post := func(path string) int {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, bytes.NewReader(big)))
		return rec.Code
	}
	// A converter upload larger than 8 MiB must reach the handler intact;
	// its own per-file/batch caps apply there.
	if got := post("/api/convert"); got != http.StatusOK {
		t.Fatalf("converter upload over 8 MiB: status %d, want 200", got)
	}
	// Every other generated endpoint keeps the JSON cap.
	if got := post("/api/auth/login"); got != http.StatusRequestEntityTooLarge {
		t.Fatalf("ordinary JSON endpoint: status %d, want the 8 MiB cap to trip", got)
	}
	// Sub-paths are not exempt by prefix.
	if got := post("/api/convert/abc"); got != http.StatusRequestEntityTooLarge {
		t.Fatalf("converter sub-path: status %d, want the JSON cap", got)
	}
}

func TestContentSecurityPolicy_HashesTheInlineThemeScript(t *testing.T) {
	body := "\n      document.documentElement.dataset.theme = \"dark\";\n    "
	ui := fstest.MapFS{"index.html": {Data: []byte(
		`<html><head><script>` + body + `</script><script type="module" crossorigin src="/assets/index.js"></script></head></html>`)}}
	csp := contentSecurityPolicy(ui)
	sum := sha256.Sum256([]byte(body))
	want := "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
	if !strings.Contains(csp, "script-src 'self' "+want+";") {
		t.Fatalf("inline script hash missing from %q", csp)
	}
	if strings.Contains(csp, "'unsafe-eval'") || strings.Contains(csp, "script-src 'self' 'unsafe-inline'") {
		t.Fatalf("CSP lets arbitrary script run: %q", csp)
	}
	// Without an embedded UI (dev builds) the policy is still well-formed.
	if !strings.Contains(contentSecurityPolicy(nil), "script-src 'self';") {
		t.Fatal("policy without UI lost its script-src")
	}
}

func TestSecurityHeaders_HSTSOnlyOverHTTPS(t *testing.T) {
	t.Cleanup(func() { api.SetTrustedProxies(nil) })
	_, proxyNet, _ := net.ParseCIDR("10.0.0.0/8")
	api.SetTrustedProxies([]*net.IPNet{proxyNet})
	h := securityHeaders("default-src 'self'")(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	hsts := func(r *http.Request) string {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Header().Get("Content-Security-Policy") == "" {
			t.Fatal("CSP header missing")
		}
		return rec.Header().Get("Strict-Transport-Security")
	}
	plain := httptest.NewRequest(http.MethodGet, "http://homehub.lan/", nil)
	plain.RemoteAddr = "192.168.1.5:4000"
	if got := hsts(plain); got != "" {
		t.Fatalf("HSTS on plain http: %q", got)
	}
	// A client can't talk us into HSTS by sending the proxy header itself.
	forged := httptest.NewRequest(http.MethodGet, "http://homehub.lan/", nil)
	forged.RemoteAddr = "192.168.1.5:4000"
	forged.Header.Set("X-Forwarded-Proto", "https")
	if got := hsts(forged); got != "" {
		t.Fatalf("HSTS from an untrusted X-Forwarded-Proto: %q", got)
	}
	proxied := httptest.NewRequest(http.MethodGet, "http://homehub.lan/", nil)
	proxied.RemoteAddr = "10.0.0.2:4000"
	proxied.Header.Set("X-Forwarded-Proto", "https")
	if got := hsts(proxied); got != hstsValue {
		t.Fatalf("HSTS behind a trusted TLS proxy: %q", got)
	}
	direct := httptest.NewRequest(http.MethodGet, "https://homehub.lan/", nil)
	direct.TLS = &tls.ConnectionState{}
	if got := hsts(direct); got != hstsValue {
		t.Fatalf("HSTS over direct TLS: %q", got)
	}
}
