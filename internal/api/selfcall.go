package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"

	"github.com/go-chi/chi/v5"

	"github.com/TheOriUHD/homehub/internal/httpctx"
)

// SelfCall performs one API request in-process under an explicit identity.
// The assistant's tools ride through here so every tool inherits the real
// handlers' RBAC checks, auditing, and error surfacing — one code path,
// whether a human clicked or a model asked.
func (s *Server) SelfCall(ctx context.Context, id *httpctx.Identity, method, path string, body any) (int, []byte) {
	s.selfOnce.Do(func() {
		strict := NewStrictHandlerWithOptions(s, nil, StrictHTTPServerOptions{})
		router := chi.NewRouter()
		HandlerWithOptions(strict, ChiServerOptions{BaseRouter: router})
		s.self = router
	})

	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return http.StatusBadRequest, []byte(`{"message":"unencodable body"}`)
		}
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	reqCtx := httpctx.WithIdentity(ctx, id)
	reqCtx = httpctx.WithHTTP(reqCtx, rec, req)
	// The outer request's chi RouteContext rides along in ctx; with it
	// present, the inner router believes it is a subrouter mid-dispatch
	// and routes the PARENT's leftover state (which decoded empty bodies
	// as "can't decode JSON body: EOF"). Blank it so routing starts fresh.
	reqCtx = context.WithValue(reqCtx, chi.RouteCtxKey, nil)
	func() {
		// A panicking handler must cost one tool result, not the turn.
		defer func() {
			if r := recover(); r != nil {
				s.logger.Error("self-call panic", "method", method, "path", path, "panic", r)
				rec.Code = http.StatusInternalServerError
				rec.Body.Reset()
				rec.Body.WriteString(`{"message":"internal error"}`)
			}
		}()
		s.self.ServeHTTP(rec, req.WithContext(reqCtx))
	}()
	if rec.Code >= 400 {
		pattern := ""
		if rc := chi.RouteContext(req.Context()); rc != nil {
			pattern = rc.RoutePattern()
		}
		s.logger.Warn("self-call failed", "method", method, "path", path, "status", rec.Code,
			"pattern", pattern, "body", truncateForLog(rec.Body.String()))
	}
	s.announceSelfChange(id, method, path, rec.Code)
	return rec.Code, rec.Body.Bytes()
}

// announceSelfChange tells the acting user's open pages that the agent just
// changed something, so the screen catches up on its own.
//
// It hooks HERE, at the one door every tool goes through, rather than in each
// handler: the agent's reach is the API's, and a rule per endpoint would be a
// list to keep in step with every tool added later. Domain topics still exist
// and are still the right thing for changes that everyone should see — this is
// the per-user backstop that makes "the agent did something" visible whether
// or not the domain it touched has a topic of its own.
//
// Reads announce nothing, and neither does a call that failed: nothing moved.
func (s *Server) announceSelfChange(id *httpctx.Identity, method, path string, code int) {
	if s.hub == nil || id == nil || !announcesChange(method, code) {
		return
	}
	s.hub.PublishUser(id.User.ID, agentTopic, map[string]any{
		"event": "changed", "method": method, "path": path,
	})
}

// announcesChange decides whether a self-call moved anything. A read changes
// nothing and a refusal changes nothing; announcing either would have every
// open page refetch each time the agent merely looked something up, which on
// a turn with six tool calls is six rounds of pointless traffic.
func announcesChange(method string, code int) bool {
	if code >= 400 {
		return false
	}
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func truncateForLog(s string) string {
	if len(s) > 300 {
		return s[:300]
	}
	return s
}
