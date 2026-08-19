package api

import (
	"context"
	"encoding/json"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/TheOriUHD/homehub/internal/httpctx"
	"github.com/TheOriUHD/homehub/internal/store"
)

// Signals: the global notification stream. Readable by every signed-in user
// (one homelab, one truth); dismissal records who cleared it.

func toAPISignal(s *store.Signal) Signal {
	out := Signal{
		Id:         s.ID,
		Collector:  s.Collector,
		SourceType: s.SourceType,
		SourceName: s.SourceName,
		Severity:   SignalSeverity(s.Severity),
		Title:      s.Title,
		Body:       s.Body,
		Count:      s.Count,
		FirstSeen:  s.FirstSeen,
		LastSeen:   s.LastSeen,
		ResolvedAt: s.ResolvedAt,
	}
	if s.SourceID != nil {
		sid := openapi_types.UUID(*s.SourceID)
		out.SourceId = &sid
	}
	if len(s.Detail) > 0 {
		var detail any
		if err := json.Unmarshal(s.Detail, &detail); err == nil {
			out.Detail = detail
		}
	}
	return out
}

func (s *Server) ListSignals(ctx context.Context, req ListSignalsRequestObject) (ListSignalsResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	// The signals surface can be hidden for a role or an individual user; such
	// accounts get an empty, zeroed stream (the UI also hides the bell/panel/page).
	if hidden, err := s.store.SignalsHiddenForUser(ctx, id.User.ID); err == nil && hidden {
		return ListSignals200JSONResponse{Items: []Signal{}, Counts: SignalCounts{}}, nil
	}
	limit := 200
	if req.Params.Limit != nil && *req.Params.Limit > 0 {
		limit = *req.Params.Limit
	}
	rows, err := s.store.ListSignals(ctx, limit)
	if err != nil {
		return nil, err
	}
	counts, err := s.store.SignalCounts(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]Signal, 0, len(rows))
	for i := range rows {
		items = append(items, toAPISignal(&rows[i]))
	}
	return ListSignals200JSONResponse{
		Items: items,
		Counts: SignalCounts{
			Info:      counts["info"],
			Warning:   counts["warning"],
			Critical:  counts["critical"],
			Recovered: counts["recovered"],
		},
	}, nil
}

func (s *Server) DismissSignals(ctx context.Context, req DismissSignalsRequestObject) (DismissSignalsResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	n, err := s.store.DismissSignals(ctx, req.Body.Ids, id.User.ID)
	if err != nil {
		return nil, err
	}
	// Everyone's bell and panel update at once.
	s.hub.Publish("signals", map[string]any{"event": "changed"})
	return DismissSignals200JSONResponse{Dismissed: n}, nil
}
