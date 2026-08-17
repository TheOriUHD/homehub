package api

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/TheOriUHD/homehub/internal/httpctx"
	"github.com/TheOriUHD/homehub/internal/store"
)

// Per-user preference documents ("overview" card layout, …). Self-scoped:
// no RBAC beyond being signed in, and never readable across users.

const maxPrefBytes = 16 * 1024

func (s *Server) GetUserPref(ctx context.Context, req GetUserPrefRequestObject) (GetUserPrefResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	raw, err := s.store.GetUserPref(ctx, id.User.ID, req.Key)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return GetUserPref200JSONResponse{Value: nil}, nil
		}
		return nil, err
	}
	var value map[string]interface{}
	if err := json.Unmarshal(raw, &value); err != nil {
		// A corrupt document behaves like an unset one rather than bricking
		// the surface that reads it.
		return GetUserPref200JSONResponse{Value: nil}, nil
	}
	return GetUserPref200JSONResponse{Value: &value}, nil
}

func (s *Server) PutUserPref(ctx context.Context, req PutUserPrefRequestObject) (PutUserPrefResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	raw, err := json.Marshal(req.Body.Value)
	if err != nil {
		return PutUserPref400JSONResponse{BadRequestJSONResponse(errBody("invalid_pref", "the document is not valid JSON"))}, nil
	}
	if len(raw) > maxPrefBytes {
		return PutUserPref400JSONResponse{BadRequestJSONResponse(errBody("pref_too_large", "preference documents are limited to 16 KB"))}, nil
	}
	if err := s.store.SetUserPref(ctx, id.User.ID, req.Key, raw); err != nil {
		return nil, err
	}
	return PutUserPref204Response{}, nil
}
