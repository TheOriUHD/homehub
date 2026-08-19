package api

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"

	"homehub/internal/httpctx"
	"homehub/internal/integrations/moonraker"
	"homehub/internal/rbac"
	"homehub/internal/store"
)

// moonrakerConn resolves an integration id to a live Moonraker connection,
// enforcing instance visibility.
func (s *Server) moonrakerConn(ctx context.Context, id *httpctx.Identity, instID uuid.UUID) (*moonraker.Conn, string, error) {
	row, err := s.store.GetIntegration(ctx, instID)
	if err != nil || row.Type != moonraker.TypeID {
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
	mc, ok := conn.(*moonraker.Conn)
	if !ok {
		return nil, resource, store.ErrNotFound
	}
	return mc, resource, nil
}

func (s *Server) MoonrakerSnapshot(ctx context.Context, req MoonrakerSnapshotRequestObject) (MoonrakerSnapshotResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, _, err := s.moonrakerConn(ctx, id, req.Id)
	if err != nil {
		if err == store.ErrNotFound {
			return MoonrakerSnapshot404JSONResponse{notFoundErr("instance")}, nil
		}
		return MoonrakerSnapshot502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	snap, serr := conn.Snapshot(ctx)
	if serr != nil {
		return MoonrakerSnapshot502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", serr.Error()))}, nil
	}
	// The domain snapshot and the API schema share identical JSON shapes
	// (verified field-by-field), so a marshal round-trip is the converter —
	// it also handles float64→float32 and the optional pointer fields.
	var out MoonrakerSnapshot
	raw, _ := json.Marshal(snap)
	if uerr := json.Unmarshal(raw, &out); uerr != nil {
		return MoonrakerSnapshot502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", uerr.Error()))}, nil
	}
	return MoonrakerSnapshot200JSONResponse(out), nil
}

// moonrakerControl resolves the connection and checks control-level access,
// the gate every mutating Moonraker action shares.
func (s *Server) moonrakerControl(ctx context.Context, req uuid.UUID) (*moonraker.Conn, string, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, "", ErrUnauthenticated
	}
	conn, resource, err := s.moonrakerConn(ctx, id, req)
	if err != nil {
		return nil, "", err
	}
	if !s.can(ctx, id, resource, rbac.LevelControl) {
		return nil, "", store.ErrNotFound
	}
	return conn, resource, nil
}

func (s *Server) MoonrakerGcode(ctx context.Context, req MoonrakerGcodeRequestObject) (MoonrakerGcodeResponseObject, error) {
	conn, resource, err := s.moonrakerControl(ctx, req.Id)
	if err != nil {
		if err == ErrUnauthenticated {
			return nil, err
		}
		if err == store.ErrNotFound {
			return MoonrakerGcode404JSONResponse{notFoundErr("instance")}, nil
		}
		return MoonrakerGcode502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	if req.Body == nil || strings.TrimSpace(req.Body.Script) == "" {
		return MoonrakerGcode400JSONResponse{BadRequestJSONResponse(errBody("bad_input", "a g-code script is required"))}, nil
	}
	if gerr := conn.RunGcode(ctx, req.Body.Script); gerr != nil {
		return MoonrakerGcode502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", gerr.Error()))}, nil
	}
	s.audit.Log(ctx, actorFor(httpctx.IdentityFrom(ctx)), "moonraker.gcode", resource, "success",
		map[string]any{"script": strings.TrimSpace(req.Body.Script)})
	return MoonrakerGcode204Response{}, nil
}

func (s *Server) MoonrakerSetTemperature(ctx context.Context, req MoonrakerSetTemperatureRequestObject) (MoonrakerSetTemperatureResponseObject, error) {
	conn, resource, err := s.moonrakerControl(ctx, req.Id)
	if err != nil {
		if err == ErrUnauthenticated {
			return nil, err
		}
		if err == store.ErrNotFound {
			return MoonrakerSetTemperature404JSONResponse{notFoundErr("instance")}, nil
		}
		return MoonrakerSetTemperature502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	if req.Body == nil || strings.TrimSpace(req.Body.Heater) == "" {
		return MoonrakerSetTemperature400JSONResponse{BadRequestJSONResponse(errBody("bad_input", "a heater is required"))}, nil
	}
	if terr := conn.SetHeaterTarget(ctx, req.Body.Heater, float64(req.Body.Target)); terr != nil {
		return MoonrakerSetTemperature502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", terr.Error()))}, nil
	}
	s.audit.Log(ctx, actorFor(httpctx.IdentityFrom(ctx)), "moonraker.set_temp", resource, "success",
		map[string]any{"heater": req.Body.Heater, "target": req.Body.Target})
	return MoonrakerSetTemperature204Response{}, nil
}

func (s *Server) MoonrakerPrintAction(ctx context.Context, req MoonrakerPrintActionRequestObject) (MoonrakerPrintActionResponseObject, error) {
	conn, resource, err := s.moonrakerControl(ctx, req.Id)
	if err != nil {
		if err == ErrUnauthenticated {
			return nil, err
		}
		if err == store.ErrNotFound {
			return MoonrakerPrintAction404JSONResponse{notFoundErr("instance")}, nil
		}
		return MoonrakerPrintAction502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	action := string(req.Body.Action)
	if perr := conn.PrintAction(ctx, action); perr != nil {
		return MoonrakerPrintAction502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", perr.Error()))}, nil
	}
	s.audit.Log(ctx, actorFor(httpctx.IdentityFrom(ctx)), "moonraker.print."+action, resource, "success", nil)
	return MoonrakerPrintAction204Response{}, nil
}

func (s *Server) MoonrakerEstop(ctx context.Context, req MoonrakerEstopRequestObject) (MoonrakerEstopResponseObject, error) {
	conn, resource, err := s.moonrakerControl(ctx, req.Id)
	if err != nil {
		if err == ErrUnauthenticated {
			return nil, err
		}
		if err == store.ErrNotFound {
			return MoonrakerEstop404JSONResponse{notFoundErr("instance")}, nil
		}
		return MoonrakerEstop502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	if eerr := conn.EmergencyStop(ctx); eerr != nil {
		return MoonrakerEstop502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", eerr.Error()))}, nil
	}
	s.audit.Log(ctx, actorFor(httpctx.IdentityFrom(ctx)), "moonraker.estop", resource, "success", nil)
	return MoonrakerEstop204Response{}, nil
}

func (s *Server) MoonrakerFirmwareRestart(ctx context.Context, req MoonrakerFirmwareRestartRequestObject) (MoonrakerFirmwareRestartResponseObject, error) {
	conn, resource, err := s.moonrakerControl(ctx, req.Id)
	if err != nil {
		if err == ErrUnauthenticated {
			return nil, err
		}
		if err == store.ErrNotFound {
			return MoonrakerFirmwareRestart404JSONResponse{notFoundErr("instance")}, nil
		}
		return MoonrakerFirmwareRestart502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	if rerr := conn.FirmwareRestart(ctx); rerr != nil {
		return MoonrakerFirmwareRestart502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", rerr.Error()))}, nil
	}
	s.audit.Log(ctx, actorFor(httpctx.IdentityFrom(ctx)), "moonraker.firmware_restart", resource, "success", nil)
	return MoonrakerFirmwareRestart204Response{}, nil
}
