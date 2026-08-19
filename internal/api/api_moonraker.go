package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/TheOriUHD/homehub/internal/httpctx"
	"github.com/TheOriUHD/homehub/internal/integrations/moonraker"
	"github.com/TheOriUHD/homehub/internal/rbac"
	"github.com/TheOriUHD/homehub/internal/store"
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

func (s *Server) MoonrakerSetWebcam(ctx context.Context, req MoonrakerSetWebcamRequestObject) (MoonrakerSetWebcamResponseObject, error) {
	conn, resource, err := s.moonrakerControl(ctx, req.Id)
	if err != nil {
		if err == ErrUnauthenticated {
			return nil, err
		}
		if err == store.ErrNotFound {
			return MoonrakerSetWebcam404JSONResponse{notFoundErr("instance")}, nil
		}
		return MoonrakerSetWebcam502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	if req.Body == nil || strings.TrimSpace(req.Body.Name) == "" {
		return MoonrakerSetWebcam400JSONResponse{BadRequestJSONResponse(errBody("bad_input", "a camera name is required"))}, nil
	}
	var rotation *int
	if req.Body.Rotation != nil {
		r := int(*req.Body.Rotation)
		rotation = &r
	}
	if werr := conn.SetWebcam(ctx, req.Body.Name, req.Body.FlipHorizontal, req.Body.FlipVertical, rotation); werr != nil {
		return MoonrakerSetWebcam502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", werr.Error()))}, nil
	}
	s.audit.Log(ctx, actorFor(httpctx.IdentityFrom(ctx)), "moonraker.webcam.set", resource, "success",
		map[string]any{"name": req.Body.Name})
	return MoonrakerSetWebcam204Response{}, nil
}

// ---- webcam proxy (raw streaming routes) ------------------------------------
//
// The browser can't load the printer's plain-HTTP MJPEG stream directly from
// HomeHub's HTTPS origin (mixed content) or from off-LAN, so the backend
// fetches it and pipes it through. These are registered as raw routes (not via
// the generated API) because they stream binary bodies of unknown length.

// HandleMoonrakerWebcamStream proxies a camera's live MJPEG stream:
// GET /api/moonraker/{id}/webcam/stream?name=<camera>
func (s *Server) HandleMoonrakerWebcamStream(w http.ResponseWriter, r *http.Request) {
	s.webcamProxy(w, r, false)
}

// HandleMoonrakerWebcamSnapshot proxies a single still frame:
// GET /api/moonraker/{id}/webcam/snapshot?name=<camera>
func (s *Server) HandleMoonrakerWebcamSnapshot(w http.ResponseWriter, r *http.Request) {
	s.webcamProxy(w, r, true)
}

func (s *Server) webcamProxy(w http.ResponseWriter, r *http.Request, snapshot bool) {
	ctx := r.Context()
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		http.Error(w, "sign in first", http.StatusUnauthorized)
		return
	}
	instID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	conn, _, err := s.moonrakerConn(ctx, id, instID) // enforces instance view
	if err != nil {
		http.NotFound(w, r)
		return
	}
	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "camera name required", http.StatusBadRequest)
		return
	}

	var target string
	var ok bool
	if snapshot {
		target, ok = conn.WebcamSnapshotURL(ctx, name)
	} else {
		target, ok = conn.WebcamStreamURL(ctx, name)
	}
	if !ok {
		http.NotFound(w, r)
		return
	}

	// A still frame gets a bounded timeout; a live stream is long-lived and
	// ends when the client disconnects (r.Context cancels the upstream fetch).
	fetchCtx := ctx
	if snapshot {
		var cancel context.CancelFunc
		fetchCtx, cancel = context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
	}
	preq, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, target, nil)
	if err != nil {
		http.Error(w, "bad camera url", http.StatusBadGateway)
		return
	}
	// The webcam is served by the machine's own web server, not Moonraker, so
	// no API key is forwarded.
	resp, err := webcamClient.Do(preq)
	if err != nil {
		http.Error(w, "camera unreachable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	// Never let a proxied frame be cached; the stream must stay live.
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(resp.StatusCode)
	copyFlush(w, resp.Body)
}

// webcamClient has no overall timeout so MJPEG streams stay open; per-request
// context deadlines bound the snapshot path and client disconnects end streams.
// CheckRedirect blocks cross-host redirects so a printer webcam that 3xx's
// elsewhere can't be used to pivot the proxy off the printer's host (SSRF).
var webcamClient = &http.Client{
	Timeout: 0,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		if len(via) > 0 && !strings.EqualFold(req.URL.Hostname(), via[0].URL.Hostname()) {
			return errors.New("cross-host redirect blocked")
		}
		return nil
	},
}

// copyFlush streams src to w, flushing after every chunk so MJPEG frames reach
// the browser as they arrive instead of buffering.
func copyFlush(w http.ResponseWriter, src io.Reader) {
	rc := http.NewResponseController(w)
	buf := make([]byte, 32*1024)
	for {
		n, rerr := src.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			_ = rc.Flush()
		}
		if rerr != nil {
			return
		}
	}
}
