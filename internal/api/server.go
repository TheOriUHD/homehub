// Package api implements the HTTP API defined in api/openapi.yaml. The
// generated strict-server interface (gen.go) guarantees this implementation
// matches the published contract.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"homehub/internal/audit"
	"homehub/internal/auth"
	backuppkg "homehub/internal/backup"
	"homehub/internal/calendar"
	"homehub/internal/convert"
	"homehub/internal/httpctx"
	"homehub/internal/integration"
	"homehub/internal/integrations/minecraft"
	"homehub/internal/integrations/patchpilot"
	"homehub/internal/jobs"
	"homehub/internal/metrics"
	"homehub/internal/rbac"
	"homehub/internal/settings"
	"homehub/internal/store"
	"homehub/internal/telegram"
	"homehub/internal/term"
	"homehub/internal/vault"
	"homehub/internal/ws"
)

// SessionCookie is the session cookie name; shared with the HTTP server's
// session middleware.
const SessionCookie = "homehub_session"

// Resource paths for the app's own administrative surfaces. These flow
// through the same RBAC engine as integration resources.
const (
	ResUsers        = "system/users"
	ResRoles        = "system/roles"
	ResSettings     = "system/settings"
	ResAudit        = "system/audit"
	ResHealth       = "system/health"
	ResIntegrations = "system/integrations"
	// ResTools guards the server-backed network utilities (HTTP tester, DNS,
	// reachability/port check, SSL check, IP scanner). It is deliberately a
	// TOP-LEVEL resource, not under "system": the built-in Operator/Viewer
	// roles carry a deny-view on "system", which would also lock Operators out.
	// At LevelControl this admits Owner + Operator and excludes read-only
	// Viewer — the intent, since these drive the server's outbound network.
	ResTools = "tools"
)

type Server struct {
	store        *store.Store
	auth         *auth.Service
	rbac         *rbac.Engine
	audit        *audit.Service
	settings     *settings.Service
	jobs         *jobs.Engine
	hub          *ws.Hub
	vault        *vault.Vault
	integrations *integration.Manager
	// digest bookkeeping: the last schedule check between minute ticks.
	digestMu        sync.Mutex
	digestLastCheck time.Time
	metrics         *metrics.Service
	telegram        *telegram.Bot
	term            *term.Manager
	backups         *backuppkg.Service
	mc              *minecraft.Registry
	patchpilot      *patchpilot.Registry
	convert         *convert.Service
	calendar        *calendar.Service
	companion       fs.FS
	dataDir         string
	logger          *slog.Logger
	started         time.Time

	// self is the lazily built in-process API handler for SelfCall.
	selfOnce sync.Once
	self     http.Handler
}

var _ StrictServerInterface = (*Server)(nil)

func NewServer(st *store.Store, authSvc *auth.Service, engine *rbac.Engine, aud *audit.Service,
	set *settings.Service, jobEngine *jobs.Engine, hub *ws.Hub, v *vault.Vault,
	integrations *integration.Manager, metricsSvc *metrics.Service, termMgr *term.Manager,
	backups *backuppkg.Service, mc *minecraft.Registry, pp *patchpilot.Registry, convertSvc *convert.Service, companionFS fs.FS, dataDir string, logger *slog.Logger) *Server {
	return &Server{
		store:        st,
		auth:         authSvc,
		rbac:         engine,
		audit:        aud,
		settings:     set,
		jobs:         jobEngine,
		hub:          hub,
		vault:        v,
		integrations: integrations,
		metrics:      metricsSvc,
		term:         termMgr,
		backups:      backups,
		mc:           mc,
		patchpilot:   pp,
		convert:      convertSvc,
		companion:    companionFS,
		dataDir:      dataDir,
		logger:       logger.With("component", "api"),
		started:      time.Now(),
	}
}

// visibleUnder wraps the engine's subtree visibility check, failing closed.
func (s *Server) visibleUnder(ctx context.Context, id *httpctx.Identity, prefix string) bool {
	ok, err := s.rbac.VisibleUnder(ctx, principal(id), prefix)
	if err != nil {
		s.logger.Error("visibility evaluation failed", "prefix", prefix, "error", err)
		return false
	}
	return ok
}

// ---- request helpers -------------------------------------------------------

// ErrUnauthenticated is returned from handlers on missing identity; the
// transport error handler maps it to a JSON 401.
var ErrUnauthenticated = errors.New("unauthenticated")

// ErrForbidden is returned from handlers when the caller is authenticated but
// lacks the required grant; the transport error handler maps it to a JSON 403.
// Used where the generated response type has no 403 variant.
var ErrForbidden = errors.New("forbidden")

func errBody(code, message string) Error {
	return Error{Code: code, Message: message}
}

func unauthorized() UnauthorizedJSONResponse {
	return UnauthorizedJSONResponse(errBody("unauthorized", "sign in to continue"))
}

func forbidden() ForbiddenJSONResponse {
	return ForbiddenJSONResponse(errBody("forbidden", "you do not have access to this"))
}

func notFoundErr(what string) NotFoundJSONResponse {
	return NotFoundJSONResponse(errBody("not_found", what+" not found"))
}

// trustedProxies are the reverse-proxy networks whose forwarding headers we
// believe. Set once at startup from config; empty means trust none.
var trustedProxies []*net.IPNet

// SetTrustedProxies configures which peer networks may set X-Forwarded-For /
// X-Real-Ip. Called once during wiring.
func SetTrustedProxies(nets []*net.IPNet) { trustedProxies = nets }

// ClientIP extracts the caller address. Reverse-proxy headers
// (X-Real-Ip / X-Forwarded-For) are honored ONLY when the direct socket peer
// is a configured trusted proxy — otherwise a direct LAN caller could forge
// its source IP, defeating the login rate-limiter and poisoning audit/session
// records. With no trusted proxies configured (the default), the client IP is
// always the real socket peer.
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if !peerIsTrustedProxy(host) {
		return host
	}
	if ip := strings.TrimSpace(r.Header.Get("X-Real-Ip")); ip != "" {
		return ip
	}
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		if first, _, _ := strings.Cut(fwd, ","); strings.TrimSpace(first) != "" {
			return strings.TrimSpace(first)
		}
	}
	return host
}

func peerIsTrustedProxy(host string) bool {
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, n := range trustedProxies {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// requestOrigin reconstructs the origin serving this request, for WebAuthn
// and invite links when no app URL is configured.
func requestOrigin(r *http.Request) string {
	if o := r.Header.Get("Origin"); o != "" && o != "null" {
		return o
	}
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func (s *Server) baseURL(ctx context.Context) string {
	if u := s.settings.AppURL(ctx); u != "" {
		return u
	}
	return requestOrigin(httpctx.Request(ctx))
}

// principal builds the RBAC principal for an identity.
func principal(id *httpctx.Identity) rbac.Principal {
	return rbac.Principal{UserID: id.User.ID, ViewAsRole: id.ViewAsRole}
}

// principalWithoutViewAs evaluates against the user's real grants even while
// a view-as preview is active.
func principalWithoutViewAs(id *httpctx.Identity) rbac.Principal {
	return rbac.Principal{UserID: id.User.ID}
}

// can evaluates a permission, failing closed on evaluation errors.
func (s *Server) can(ctx context.Context, id *httpctx.Identity, resource string, level rbac.Level) bool {
	ok, err := s.rbac.Can(ctx, principal(id), resource, level)
	if err != nil {
		s.logger.Error("permission evaluation failed", "resource", resource, "error", err)
		return false
	}
	return ok
}

// deniedAudit records a permission denial on a mutating action.
func (s *Server) deniedAudit(ctx context.Context, id *httpctx.Identity, action, resource string) {
	s.audit.Log(ctx, actorFor(id), action, resource, audit.OutcomeDenied, nil)
}

func actorFor(id *httpctx.Identity) audit.Actor {
	return audit.Actor{
		Kind:      "user",
		ID:        &id.User.ID,
		Name:      id.User.Username,
		SessionID: &id.SessionID,
		IP:        id.IP,
	}
}

// setSessionCookie writes the session cookie onto the in-flight response.
func setSessionCookie(ctx context.Context, token string, expires time.Time) {
	r := httpctx.Request(ctx)
	secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	http.SetCookie(httpctx.Writer(ctx), &http.Cookie{
		Name:     SessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  expires,
	})
}

func clearSessionCookie(ctx context.Context) {
	http.SetCookie(httpctx.Writer(ctx), &http.Cookie{
		Name:     SessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// ---- model mapping ---------------------------------------------------------

func toAPIUser(u *store.User, roleIDs []uuid.UUID, totpEnabled bool, passkeyCount int) User {
	if roleIDs == nil {
		roleIDs = []uuid.UUID{}
	}
	return User{
		Id:                 u.ID,
		Username:           u.Username,
		DisplayName:        u.DisplayName,
		Email:              u.Email,
		Status:             UserStatus(u.Status),
		MustChangePassword: u.MustChangePassword,
		HideSignals:        u.HideSignals,
		TotpEnabled:        totpEnabled,
		PasskeyCount:       passkeyCount,
		RoleIds:            roleIDs,
		LastLoginAt:        u.LastLoginAt,
		CreatedAt:          u.CreatedAt,
	}
}

// apiUserFor loads the security aggregates for one user and maps it.
func (s *Server) apiUserFor(ctx context.Context, u *store.User) (User, error) {
	roleIDs, totpEnabled, passkeys, err := s.store.UserSecurityInfoFor(ctx, u.ID)
	if err != nil {
		return User{}, err
	}
	return toAPIUser(u, roleIDs, totpEnabled, passkeys), nil
}

func toAPIGrants(grants []store.Grant) []Grant {
	out := make([]Grant, 0, len(grants))
	for _, g := range grants {
		levels := make([]GrantLevels, 0, len(g.Levels))
		for _, l := range g.Levels {
			levels = append(levels, GrantLevels(l))
		}
		out = append(out, Grant{Selector: g.Selector, Levels: levels, Effect: GrantEffect(g.Effect)})
	}
	return out
}

// grantSpecsFromInput validates and converts grant input, returning a
// user-facing message when invalid.
func grantSpecsFromInput(inputs []GrantInput) ([]store.GrantSpec, string) {
	specs := make([]store.GrantSpec, 0, len(inputs))
	for _, in := range inputs {
		if !rbac.ValidSelector(in.Selector) {
			return nil, "invalid selector " + in.Selector + ": use slash-separated segments, '*' for one segment"
		}
		levels := make([]string, 0, len(in.Levels))
		for _, l := range in.Levels {
			levels = append(levels, string(l))
		}
		if !rbac.ValidLevels(levels) {
			return nil, "grants need at least one of view, control, configure, delete"
		}
		effect := "allow"
		if in.Effect != nil {
			effect = string(*in.Effect)
		}
		specs = append(specs, store.GrantSpec{Selector: in.Selector, Levels: levels, Effect: effect})
	}
	return specs, ""
}

func toPayload(v any) (WebauthnPayload, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var payload WebauthnPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// meFor assembles the Me response for an identity.
func (s *Server) meFor(ctx context.Context, id *httpctx.Identity) (Me, error) {
	user, err := s.apiUserFor(ctx, id.User)
	if err != nil {
		return Me{}, err
	}
	var grants []store.Grant
	if id.ViewAsRole != nil {
		grants, err = s.store.RoleGrantsOnly(ctx, *id.ViewAsRole)
	} else {
		grants, err = s.store.UserGrants(ctx, id.User.ID)
	}
	if err != nil {
		return Me{}, err
	}
	hidden, err := s.store.SignalsHiddenForUser(ctx, id.User.ID)
	if err != nil {
		return Me{}, err
	}
	me := Me{User: user, Grants: toAPIGrants(grants), SignalsHidden: hidden}
	me.ViewAsRoleId = id.ViewAsRole
	return me, nil
}
