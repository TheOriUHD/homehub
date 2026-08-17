package api

import (
	"context"

	"github.com/google/uuid"

	"github.com/TheOriUHD/homehub/internal/httpctx"
	"github.com/TheOriUHD/homehub/internal/integrations/aperture"
	"github.com/TheOriUHD/homehub/internal/rbac"
	"github.com/TheOriUHD/homehub/internal/store"
)

// apertureConn resolves an integration id to a live Aperture connection,
// enforcing instance visibility.
func (s *Server) apertureConn(ctx context.Context, id *httpctx.Identity, instID uuid.UUID) (*aperture.Conn, string, error) {
	row, err := s.store.GetIntegration(ctx, instID)
	if err != nil || row.Type != aperture.TypeID {
		return nil, "", store.ErrNotFound
	}
	resource := row.Type + "/" + instID.String()
	if !s.visibleUnder(ctx, id, resource) {
		return nil, "", store.ErrNotFound
	}
	conn, err := s.integrations.Connection(instID)
	if err != nil {
		return nil, resource, err
	}
	ac, ok := conn.(*aperture.Conn)
	if !ok {
		return nil, resource, store.ErrNotFound
	}
	return ac, resource, nil
}

func (s *Server) ApertureOverview(ctx context.Context, req ApertureOverviewRequestObject) (ApertureOverviewResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, resource, err := s.apertureConn(ctx, id, req.Id)
	if err == store.ErrNotFound {
		return ApertureOverview404JSONResponse{notFoundErr("system")}, nil
	}
	if err != nil {
		return ApertureOverview502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}

	cameras, err := conn.Cameras(ctx)
	if err != nil {
		return ApertureOverview502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	// Health and storage are merged per-camera; either failing alone should
	// degrade the answer, not blank it.
	healthByID := map[string]aperture.CameraHealth{}
	if status, herr := conn.Status(ctx); herr == nil {
		for _, h := range status {
			healthByID[h.CameraID] = h
		}
	}
	var capBytes *int64
	storageByID := map[string]aperture.CameraStorage{}
	if storage, serr := conn.StorageInfo(ctx); serr == nil {
		cap := storage.CapBytes
		capBytes = &cap
		for _, cs := range storage.Cameras {
			storageByID[cs.CameraID] = cs
		}
	}

	out := ApertureOverview{
		BaseUrl:  conn.BaseURL(),
		Cameras:  []ApertureCamera{},
		CapBytes: capBytes,
		Bitrate: []struct {
			Bps int64 `json:"bps"`
			Ts  int64 `json:"ts"`
		}{},
	}
	for _, cam := range cameras {
		// Per-camera RBAC: hidden, not disabled.
		if !s.can(ctx, id, resource+"/camera/"+cam.ID, rbac.LevelView) {
			continue
		}
		entry := ApertureCamera{
			Id:         cam.ID,
			Name:       cam.Name,
			Enabled:    cam.Enabled,
			RecordMode: cam.RecordMode,
			SortOrder:  cam.SortOrder,
		}
		if h, ok := healthByID[cam.ID]; ok {
			entry.RecordingOk = h.RecordingOk
			entry.LiveOk = h.LiveOk
			entry.CoveragePercent = h.CoveragePercent
			entry.WatchdogStale = h.WatchdogStale
			entry.RestartsToday = h.RestartsToday
			if h.LastSegmentTs > 0 {
				ts := h.LastSegmentTs
				entry.LastSegmentTs = &ts
			}
		}
		if cs, ok := storageByID[cam.ID]; ok {
			bytes, oldest, newest := cs.Bytes, cs.OldestTs, cs.NewestTs
			entry.StorageBytes = &bytes
			if oldest > 0 {
				entry.OldestTs = &oldest
			}
			if newest > 0 {
				entry.NewestTs = &newest
			}
		}
		out.Cameras = append(out.Cameras, entry)
	}

	if buckets, berr := conn.Bitrate(ctx); berr == nil {
		for _, b := range buckets {
			out.Bitrate = append(out.Bitrate, struct {
				Bps int64 `json:"bps"`
				Ts  int64 `json:"ts"`
			}{Bps: b.Bps, Ts: b.Ts})
		}
	}
	return ApertureOverview200JSONResponse(out), nil
}

func (s *Server) ApertureStreamToken(ctx context.Context, req ApertureStreamTokenRequestObject) (ApertureStreamTokenResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, resource, err := s.apertureConn(ctx, id, req.Id)
	if err == store.ErrNotFound {
		return ApertureStreamToken404JSONResponse{notFoundErr("system")}, nil
	}
	if err != nil {
		return ApertureStreamToken502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	if !s.can(ctx, id, resource+"/camera/"+req.CameraId, rbac.LevelView) {
		return ApertureStreamToken404JSONResponse{notFoundErr("camera")}, nil
	}
	grant, err := conn.MintStreamToken(ctx, req.CameraId)
	if err != nil {
		return ApertureStreamToken502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	return ApertureStreamToken200JSONResponse(ApertureStreamGrant{
		WsUrl:  grant.WsURL,
		HasSub: grant.HasSub,
	}), nil
}
