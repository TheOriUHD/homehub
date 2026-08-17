package pvesim

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

// j is shorthand for building wire-shaped JSON objects.
type j = map[string]any

// Handler returns the /api2/json HTTP surface of the simulator.
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	notImpl := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Loud 501 so client gaps surface instead of silently 404ing.
		writeJSON(w, http.StatusNotImplemented, j{"errors": j{"path": "not implemented in pvesim"}})
	})
	r.NotFound(notImpl)
	r.MethodNotAllowed(notImpl)

	r.Route("/api2/json", func(r chi.Router) {
		r.NotFound(notImpl)
		r.MethodNotAllowed(notImpl)
		r.Use(s.requireToken)

		r.Get("/version", s.getVersion)
		r.Get("/cluster/status", s.getClusterStatus)
		r.Get("/cluster/resources", s.getClusterResources)
		r.Get("/cluster/backup", s.getBackupJobs)
		r.Post("/cluster/backup", s.postBackupJob)
		r.Put("/cluster/backup/{id}", s.putBackupJob)
		r.Get("/nodes", s.getNodes)

		r.Route("/nodes/{node}", func(r chi.Router) {
			r.Get("/status", s.getNodeStatus)
			r.Get("/storage", s.getNodeStorage)
			r.Get("/qemu", s.getNodeGuests("qemu"))
			r.Get("/lxc", s.getNodeGuests("lxc"))
			r.Get("/tasks", s.getTasks)
			r.Get("/tasks/{upid}/status", s.getTaskStatus)
			r.Get("/tasks/{upid}/log", s.getTaskLog)
			r.Post("/vzdump", s.postVzdump)
			// Real PVE serves guest console websockets ONLY at the
			// guest-scoped path; the node-level endpoint is the node shell.
			// Mirroring that keeps wrong-path dials from passing here.

			r.Route("/{kind:qemu|lxc}/{vmid:[0-9]+}", func(r chi.Router) {
				r.Delete("/", s.deleteGuest)
				r.Get("/status/current", s.getGuestStatus)
				r.Post("/status/{action}", s.postGuestAction)
				r.Get("/config", s.getGuestConfig)
				// Real PVE exposes POST for qemu and PUT for lxc; accept both
				// methods for both kinds so clients of either style work.
				r.Post("/config", s.setGuestConfig)
				r.Put("/config", s.setGuestConfig)
				r.Get("/snapshot", s.getSnapshots)
				r.Post("/snapshot", s.postSnapshot)
				r.Post("/snapshot/{name}/rollback", s.postSnapshotRollback)
				r.Delete("/snapshot/{name}", s.deleteSnapshot)
				r.Post("/clone", s.postClone)
				r.Post("/migrate", s.postMigrate)
				r.Post("/vncproxy", s.postVNCProxy)
				r.Post("/termproxy", s.postTermProxy)
				r.Get("/agent/network-get-interfaces", s.getAgentInterfaces)
				r.Get("/agent/get-host-name", s.getAgentHostname)
				r.Get("/agent/get-osinfo", s.getAgentOSInfo)
				r.Get("/vncwebsocket", s.getVNCWebsocket)
			})
		})
	})
	return r
}

func (s *Server) requireToken(next http.Handler) http.Handler {
	want := "PVEAPIToken=" + s.token
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimSpace(r.Header.Get("Authorization")) != want {
			writeJSON(w, http.StatusUnauthorized, j{"data": nil, "message": "authentication failure"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json;charset=UTF-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeData wraps v in the {"data": ...} envelope every PVE response uses.
func writeData(w http.ResponseWriter, v any) {
	writeJSON(w, http.StatusOK, j{"data": v})
}

// writeErr mimics PVE's error responses: an HTTP error status with null data
// and a human-readable message.
func writeErr(w http.ResponseWriter, status int, format string, args ...any) {
	writeJSON(w, status, j{"data": nil, "message": fmt.Sprintf(format, args...)})
}

// parseBody accepts form-encoded or JSON bodies (PVE clients send both) and
// flattens all values to strings. Query parameters are merged in for form
// requests, matching PVE's parameter handling.
func parseBody(r *http.Request) (map[string]string, error) {
	out := map[string]string{}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		var m map[string]any
		if err := json.NewDecoder(r.Body).Decode(&m); err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		for k, v := range m {
			switch t := v.(type) {
			case string:
				out[k] = t
			case bool:
				out[k] = strconv.Itoa(boolToInt(t))
			case float64:
				if t == math.Trunc(t) {
					out[k] = strconv.FormatInt(int64(t), 10)
				} else {
					out[k] = strconv.FormatFloat(t, 'f', -1, 64)
				}
			default:
				out[k] = fmt.Sprint(v)
			}
		}
		return out, nil
	}
	if err := r.ParseForm(); err != nil {
		return nil, err
	}
	for k := range r.Form {
		out[k] = r.Form.Get(k)
	}
	return out, nil
}

func pathVmid(r *http.Request) int {
	n, _ := strconv.Atoi(chi.URLParam(r, "vmid"))
	return n
}
