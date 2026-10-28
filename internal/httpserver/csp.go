package httpserver

import (
	"crypto/sha256"
	"encoding/base64"
	"io/fs"
	"net/http"
	"regexp"
	"strings"

	"homehub/internal/api"
)

// inlineScript matches the classic inline <script> blocks of index.html (the
// pre-paint theme snippet); <script src=…> tags carry no body and are
// covered by 'self'.
var inlineScript = regexp.MustCompile(`(?s)<script>(.*?)</script>`)

// contentSecurityPolicy builds the CSP for the built SPA. What each directive
// admits, and why:
//
//   - script-src: our own bundle plus the exact hash of each inline script
//     in index.html (computed from the embedded file, so a rebuilt snippet
//     can't silently break). No 'unsafe-inline' and no eval — this is the
//     directive that turns an injected <script> or a stray innerHTML into a
//     no-op.
//   - style-src 'unsafe-inline': xterm, CodeMirror and the markdown editor
//     inject <style> elements at runtime. Inline style can't run code.
//   - img-src *: user markdown and integrations show pictures from anywhere,
//     and images can't run code either. data:/blob: for QR codes, EXIF
//     previews and noVNC's cursor.
//   - connect-src ws: wss:: the NVR live view opens its WebSocket straight to
//     the Aperture server, whose host is per-installation.
//   - worker-src/media-src blob:: the regex tool's worker and the NVR's
//     MediaSource video are blob URLs.
//   - frame-ancestors 'self' matches X-Frame-Options: SAMEORIGIN.
func contentSecurityPolicy(ui fs.FS) string {
	scripts := []string{"'self'"}
	if ui != nil {
		if index, err := fs.ReadFile(ui, "index.html"); err == nil {
			for _, m := range inlineScript.FindAllSubmatch(index, -1) {
				sum := sha256.Sum256(m[1])
				scripts = append(scripts, "'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'")
			}
		}
	}
	return strings.Join([]string{
		"default-src 'self'",
		"script-src " + strings.Join(scripts, " "),
		"style-src 'self' 'unsafe-inline'",
		"img-src * data: blob:",
		"font-src 'self' data:",
		"connect-src 'self' ws: wss:",
		"worker-src 'self' blob:",
		"media-src 'self' blob:",
		"frame-src 'self'",
		"object-src 'none'",
		"base-uri 'self'",
		"form-action 'self'",
		"frame-ancestors 'self'",
	}, "; ")
}

// hstsValue is sent only on requests that arrived over HTTPS. One year,
// without includeSubDomains or preload: a homelab domain often has other
// hosts that still speak plain HTTP, and preload is not undoable in a hurry.
const hstsValue = "max-age=31536000"

// securityHeaders sets defense-in-depth response headers on every route
// (SPA shell, API, helper). SAMEORIGIN (not DENY) so any same-origin
// embedding the app may add later keeps working; cross-origin framing —
// the clickjacking vector — is still blocked. HSTS only goes out over HTTPS:
// sending it on plain http is ignored by browsers anyway, and running on the
// LAN over http stays supported.
func securityHeaders(csp string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Frame-Options", "SAMEORIGIN")
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Referrer-Policy", "no-referrer")
			h.Set("Content-Security-Policy", csp)
			if api.RequestIsHTTPS(r) {
				h.Set("Strict-Transport-Security", hstsValue)
			}
			next.ServeHTTP(w, r)
		})
	}
}
