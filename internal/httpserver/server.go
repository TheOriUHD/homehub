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
	// UI is the embedded frontend; nil in development builds, where the Vite
	// dev server serves the frontend and proxies /api here.
	UI fs.FS
}

func Handler(deps Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(recoverer(deps.Logger))

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "ok")
	})

	r.Route("/api", func(apiRouter chi.Router) {
		apiRouter.Use(withHTTPContext)
		apiRouter.Use(sessionMiddleware(deps))
		apiRouter.Use(originGuard(deps))

		apiRouter.Get("/ws", wsHandler(deps))
		apiRouter.Get("/term/{sessionID}", termHandler(deps))
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
		session, ok := deps.Term.Take(chi.URLParam(r, "sessionID"), id.User.ID)
		if !ok {
			writeJSONError(w, http.StatusNotFound, "session_expired",
				"terminal session expired or already used — reopen the console")
			return
		}
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

func requestErrorHandler(w http.ResponseWriter, _ *http.Request, err error) {
	writeJSONError(w, http.StatusBadRequest, "bad_request", err.Error())
}

func responseErrorHandler(logger *slog.Logger) func(http.ResponseWriter, *http.Request, error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		if errors.Is(err, api.ErrUnauthenticated) {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized", "sign in to continue")
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
