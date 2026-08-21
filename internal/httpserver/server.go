// Package httpserver assembles the HTTP surface: middleware, the generated
// API handler, the WebSocket endpoint, the OpenAPI document, and the embedded
// frontend.
package httpserver

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"

	"homehub/internal/api"
	"homehub/internal/auth"
	"homehub/internal/httpctx"
	"homehub/internal/integrations/minecraft"
	"homehub/internal/settings"
	"homehub/internal/store"
	"homehub/internal/term"
	"homehub/internal/ws"
)

type Deps struct {
	Logger   *slog.Logger
	Auth     *auth.Service
	API      *api.Server
	Hub      *ws.Hub
	Settings *settings.Service
	Term     *term.Manager
	// MC terminates the Minecraft companion agent WebSocket.
	MC *minecraft.Registry
	// UI is the embedded frontend; nil in development builds, where the Vite
	// dev server serves the frontend and proxies /api here.
	UI fs.FS
	// Helper serves the caddy-reader installer and binaries (nil without
	// the helper build tag). Deliberately unauthenticated: the binary is
	// not a secret, and curl cannot log in.
	Helper fs.FS
}

func Handler(deps Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(recoverer(deps.Logger))
	r.Use(securityHeaders)

	if deps.Helper != nil {
		registerHelperRoutes(r, deps.Helper)
	}
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "ok")
	})

	r.Route("/api", func(apiRouter chi.Router) {
		apiRouter.Use(withHTTPContext)
		apiRouter.Use(jsonBodyLimit)
		apiRouter.Use(sessionMiddleware(deps))
		apiRouter.Use(originGuard(deps))

		apiRouter.Get("/ws", wsHandler(deps))
		apiRouter.Get("/term/{sessionID}", termHandler(deps))
		// Workspace cloud: multipart uploads of any file type, raw downloads.
		// Registered directly (not via the generated API) so bodies stream.
		apiRouter.Post("/files/upload", deps.API.HandleFileUpload)
		apiRouter.Get("/files/{id}/download", deps.API.HandleFileDownload)
		// IP scanner: streams NDJSON results as hosts complete.
		apiRouter.Post("/netscan", deps.API.HandleNetscan)
		// Assistant: streamed turn (NDJSON) + confirmed write actions.
		apiRouter.Post("/assistant/chat/stream", deps.API.HandleAssistantChatStream)
		apiRouter.Post("/assistant/actions/apply", deps.API.HandleAssistantApply)
		// Moonraker webcams: proxy the printer's MJPEG stream / snapshot so the
		// browser can show it over HTTPS and from off-LAN (registered raw so the
		// long-lived stream body isn't buffered by the generated handlers).
		apiRouter.Get("/moonraker/{id}/webcam/stream", deps.API.HandleMoonrakerWebcamStream)
		apiRouter.Get("/moonraker/{id}/webcam/snapshot", deps.API.HandleMoonrakerWebcamSnapshot)
		// Companion agents authenticate with pairing codes/tokens, not sessions.
		apiRouter.Get("/mc/agent", func(w http.ResponseWriter, r *http.Request) {
			deps.MC.HandleAgent(w, r, api.ClientIP(r))
		})
		apiRouter.Get("/openapi.json", specHandler(deps.Logger))

		strict := api.NewStrictHandlerWithOptions(deps.API, nil, api.StrictHTTPServerOptions{
			RequestErrorHandlerFunc:  requestErrorHandler,
			ResponseErrorHandlerFunc: responseErrorHandler(deps.Logger),
		})
		api.HandlerWithOptions(strict, api.ChiServerOptions{
			BaseRouter:       apiRouter,
			ErrorHandlerFunc: requestErrorHandler,
		})
	})

	r.NotFound(spaHandler(deps.UI))
	return r
}

// ---- middleware ------------------------------------------------------------

// securityHeaders sets constant defense-in-depth response headers on every
// route (SPA shell, API, helper). SAMEORIGIN (not DENY) so any same-origin
// embedding the app may add later keeps working; cross-origin framing —
// the clickjacking vector — is still blocked.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Frame-Options", "SAMEORIGIN")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// maxJSONBody caps ordinary JSON request bodies. Generous for legitimate API
// payloads, small enough that a single request can't exhaust memory.
const maxJSONBody = 8 << 20 // 8 MiB

// streamingBodyPaths are the raw routes that legitimately stream large or
// bespoke bodies and set their OWN http.MaxBytesReader; the generic JSON cap
// must not wrap them (it would truncate a 512 MiB upload at 8 MiB).
var streamingBodyPaths = map[string]bool{
	"/api/files/upload":            true,
	"/api/netscan":                 true,
	"/api/assistant/chat/stream":   true,
	"/api/assistant/actions/apply": true,
}

// jsonBodyLimit wraps the request body of the 116 generated JSON endpoints in
// a MaxBytesReader so an unbounded (even pre-auth) POST can't OOM the process.
// Runs before sessionMiddleware so /api/auth/login is covered too.
func jsonBodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil && !streamingBodyPaths[r.URL.Path] {
			r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
		}
		next.ServeHTTP(w, r)
	})
}

func withHTTPContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(httpctx.WithHTTP(r.Context(), w, r)))
	})
}

// sessionMiddleware resolves the session cookie into an identity and gates
// accounts in the forced-password-change state to the endpoints that state
// needs.
func sessionMiddleware(deps Deps) func(http.Handler) http.Handler {
	mustChangeAllowed := map[string]bool{
		"/api/auth/password": true,
		"/api/auth/me":       true,
		"/api/auth/logout":   true,
		"/api/setup/status":  true,
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(api.SessionCookie)
			if err == nil && cookie.Value != "" {
				sess, user, verr := deps.Auth.ValidateSession(r.Context(), cookie.Value, api.ClientIP(r))
				if verr == nil {
					id := &httpctx.Identity{
						User:       user,
						SessionID:  sess.ID,
						ViewAsRole: sess.ViewAsRoleID,
						IP:         api.ClientIP(r),
						UserAgent:  r.UserAgent(),
					}
					if user.MustChangePassword && !mustChangeAllowed[r.URL.Path] {
						writeJSONError(w, http.StatusForbidden, "password_change_required",
							"set a new password before continuing")
						return
					}
					r = r.WithContext(httpctx.WithIdentity(r.Context(), id))
				} else if !errors.Is(verr, store.ErrNotFound) {
					deps.Logger.Error("session validation failed", "error", verr)
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// originGuard blocks cross-site state-changing requests. Browser requests
// carry Sec-Fetch-Site and/or Origin; requests without either (curl, scripts)
// cannot be driven by a victim's browser and pass through.
func originGuard(deps Deps) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			}
			if site := r.Header.Get("Sec-Fetch-Site"); site != "" {
				if site == "same-origin" || site == "none" {
					next.ServeHTTP(w, r)
					return
				}
				writeJSONError(w, http.StatusForbidden, "cross_site_blocked", "cross-site requests are not allowed")
				return
			}
			origin := r.Header.Get("Origin")
			if origin == "" || origin == "null" {
				next.ServeHTTP(w, r)
				return
			}
			if originAllowed(origin, r, deps.Settings.AppURL(r.Context())) {
				next.ServeHTTP(w, r)
				return
			}
			writeJSONError(w, http.StatusForbidden, "cross_site_blocked", "cross-site requests are not allowed")
		})
	}
}

func originAllowed(origin string, r *http.Request, appURL string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if strings.EqualFold(u.Host, r.Host) {
		return true
	}
	if appURL != "" {
		if au, err := url.Parse(appURL); err == nil && strings.EqualFold(u.Host, au.Host) {
			return true
		}
	}
	return false
}

func recoverer(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("panic in request handler",
						"path", r.URL.Path, "panic", rec, "stack", string(debug.Stack()))
					writeJSONError(w, http.StatusInternalServerError, "internal", "something went wrong — check the server logs")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// ---- endpoints -------------------------------------------------------------

func wsHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := httpctx.IdentityFrom(r.Context())
		if id == nil {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized", "sign in to continue")
			return
		}
		patterns := []string{r.Host}
		if appURL := deps.Settings.AppURL(r.Context()); appURL != "" {
			if u, err := url.Parse(appURL); err == nil && u.Host != "" {
				patterns = append(patterns, u.Host)
			}
		}
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: patterns})
		if err != nil {
			return // Accept already replied
		}
		deps.Hub.Serve(r.Context(), conn, id.User.ID, id.ViewAsRole)
		conn.Close(websocket.StatusNormalClosure, "bye")
	}
}

// termHandler upgrades a one-time terminal session ticket to its bridged
// WebSocket (SSH shell, VNC console, or serial console).
func termHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := httpctx.IdentityFrom(r.Context())
		if id == nil {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized", "sign in to continue")
			return
		}
		sessionID := chi.URLParam(r, "sessionID")
		session, ok := deps.Term.Take(sessionID, id.User.ID)
		if !ok {
			// This used to fail silently, which made "the console just doesn't
			// connect" impossible to diagnose: a consumed ticket (double dial)
			// and an expired one look identical from the browser.
			deps.Logger.Warn("terminal session rejected",
				"session", sessionID, "user", id.User.ID, "origin", r.Header.Get("Origin"))
			writeJSONError(w, http.StatusNotFound, "session_expired",
				"terminal session expired or already used — reopen the console")
			return
		}
		deps.Logger.Info("terminal session opened", "session", sessionID, "user", id.User.ID)
		patterns := []string{r.Host}
		if appURL := deps.Settings.AppURL(r.Context()); appURL != "" {
			if u, err := url.Parse(appURL); err == nil && u.Host != "" {
				patterns = append(patterns, u.Host)
			}
		}
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			OriginPatterns: patterns,
			// Terminal streams are raw byte pipes; permessage-deflate only
			// adds latency here.
			CompressionMode: websocket.CompressionDisabled,
		})
		if err != nil {
			// Most often an Origin the patterns don't cover — silent until now.
			deps.Logger.Warn("terminal websocket upgrade failed",
				"error", err, "origin", r.Header.Get("Origin"), "patterns", patterns)
			return
		}
		if err := session.Open(r.Context(), conn); err != nil {
			deps.Logger.Warn("terminal session ended with error", "error", err)
			term.SendControl(r.Context(), conn, term.ControlFrame{Type: "error", Message: err.Error()})
		}
		conn.Close(websocket.StatusNormalClosure, "session closed")
	}
}

func specHandler(logger *slog.Logger) http.HandlerFunc {
	var cached []byte
	return func(w http.ResponseWriter, r *http.Request) {
		if cached == nil {
			swagger, err := api.GetSwagger()
			if err != nil {
				logger.Error("load embedded openapi spec", "error", err)
				writeJSONError(w, http.StatusInternalServerError, "internal", "spec unavailable")
				return
			}
			cached, err = swagger.MarshalJSON()
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, "internal", "spec unavailable")
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(cached)
	}
}

// spaHandler serves the embedded frontend with an index.html fallback for
// client-side routes.
func spaHandler(ui fs.FS) http.HandlerFunc {
	if ui == nil {
		return func(w http.ResponseWriter, r *http.Request) {
			writeJSONError(w, http.StatusNotFound, "no_ui",
				"this build has no embedded UI — in development run the frontend with `make dev-web`")
		}
	}
	fileServer := http.FileServer(http.FS(ui))
	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		if f, err := ui.Open(path); err == nil {
			_ = f.Close()
			// Hashed assets are immutable; index.html must always revalidate.
			if strings.HasPrefix(path, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}
			fileServer.ServeHTTP(w, r)
			return
		}
		index, err := fs.ReadFile(ui, "index.html")
		if err != nil {
			http.Error(w, "frontend missing from build", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(index)
	}
}

// ---- error writing ---------------------------------------------------------

func requestErrorHandler(w http.ResponseWriter, _ *http.Request, _ error) {
	// The raw err (JSON parser positions, wrapped store/provider messages) is
	// not echoed to the caller — that leaks internal structure. Strict handlers
	// that want a specific client message return a typed 4xx response instead.
	writeJSONError(w, http.StatusBadRequest, "bad_request", "invalid request")
}

func responseErrorHandler(logger *slog.Logger) func(http.ResponseWriter, *http.Request, error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		if errors.Is(err, api.ErrUnauthenticated) {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized", "sign in to continue")
			return
		}
		if errors.Is(err, api.ErrForbidden) {
			writeJSONError(w, http.StatusForbidden, "forbidden", "you do not have permission for this")
			return
		}
		logger.Error("request failed", "method", r.Method, "path", r.URL.Path, "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal", "something went wrong — check the server logs")
	}
}

func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "message": message})
}

// NewServer wraps the handler in an http.Server with sane timeouts. Write
// timeout is deliberately absent: the WebSocket and CSV export endpoints hold
// long-lived response streams.
func NewServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
}

// registerHelperRoutes serves the caddy-reader install contract: the
// installer script with __BASE_URL__ replaced by the request's own origin,
// the three static binaries, and the checksum file.
func registerHelperRoutes(r chi.Router, helperFS fs.FS) {
	r.Get("/helper/install.sh", func(w http.ResponseWriter, req *http.Request) {
		raw, err := fs.ReadFile(helperFS, "install.sh")
		if err != nil {
			http.NotFound(w, req)
			return
		}
		scheme := "http"
		if req.TLS != nil || req.Header.Get("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
		origin := scheme + "://" + req.Host
		body := strings.ReplaceAll(string(raw), "__BASE_URL__", origin)
		w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
		_, _ = w.Write([]byte(body))
	})
	for _, name := range []string{
		"caddy-reader-linux-amd64", "caddy-reader-linux-arm64", "caddy-reader-linux-armv7", "SHA256SUMS",
	} {
		name := name
		r.Get("/helper/"+name, func(w http.ResponseWriter, req *http.Request) {
			raw, err := fs.ReadFile(helperFS, name)
			if err != nil {
				http.NotFound(w, req)
				return
			}
			if name == "SHA256SUMS" {
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			} else {
				w.Header().Set("Content-Type", "application/octet-stream")
			}
			_, _ = w.Write(raw)
		})
	}
}
