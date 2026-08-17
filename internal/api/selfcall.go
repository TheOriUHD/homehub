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
	return rec.Code, rec.Body.Bytes()
}

func truncateForLog(s string) string {
	if len(s) > 300 {
		return s[:300]
	}
	return s
}
