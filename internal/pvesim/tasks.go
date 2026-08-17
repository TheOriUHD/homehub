package pvesim

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

type task struct {
	upid   string
	node   string
	pid    int
	pstart int
	start  int64
	ttype  string
	id     string
	status string // "running" | "stopped"
	exit   string
	end    int64
	log    []string
}

// startTaskLocked registers a running task and spawns a goroutine that
// completes it after delay, applying the state mutation under s.mu so task
// polling observes the transition. Caller must hold s.mu.
func (s *Server) startTaskLocked(nodeName, ttype, id string, delay time.Duration, lines []string, apply func()) string {
	s.pstart++
	t := &task{
		node:   nodeName,
		ttype:  ttype,
		id:     id,
		pid:    1000 + s.rng.IntN(60000),
		pstart: 0x1a2b00 + s.pstart,
		start:  time.Now().Unix(),
		status: "running",
		log:    lines,
	}
	t.upid = fmt.Sprintf("UPID:%s:%08X:%08X:%08X:%s:%s:root@pam:", nodeName, t.pid, t.pstart, t.start, ttype, id)
	s.tasks = append([]*task{t}, s.tasks...)
	s.byUPID[t.upid] = t

	go func() {
		time.Sleep(delay)
		s.mu.Lock()
		defer s.mu.Unlock()
		if apply != nil {
			apply()
		}
		t.status = "stopped"
		t.exit = "OK"
		t.end = time.Now().Unix()
		t.log = append(t.log, "TASK OK")
	}()
	return t.upid
}

func (s *Server) getTasks(w http.ResponseWriter, r *http.Request) {
	nodeName := chi.URLParam(r, "node")
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	items := []j{}
	for _, t := range s.tasks {
		if t.node != nodeName || len(items) >= limit {
			continue
		}
		item := j{
			"upid": t.upid, "node": t.node, "pid": t.pid, "pstart": t.pstart,
			"starttime": t.start, "type": t.ttype, "id": t.id, "user": "root@pam",
		}
		if t.status == "stopped" {
			item["status"] = t.exit
			item["endtime"] = t.end
		}
		items = append(items, item)
	}
	writeData(w, items)
}

func (s *Server) taskByUPID(w http.ResponseWriter, r *http.Request) *task {
	upid := chi.URLParam(r, "upid")
	t := s.byUPID[upid]
	if t == nil {
		writeErr(w, http.StatusInternalServerError, "no such task '%s'", upid)
	}
	return t
}

func (s *Server) getTaskStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.taskByUPID(w, r)
	if t == nil {
		return
	}
	resp := j{
		"upid": t.upid, "node": t.node, "pid": t.pid, "pstart": t.pstart,
		"starttime": t.start, "type": t.ttype, "id": t.id, "user": "root@pam",
		"status": t.status,
	}
	if t.status == "stopped" {
		resp["exitstatus"] = t.exit
	}
	writeData(w, resp)
}

func (s *Server) getTaskLog(w http.ResponseWriter, r *http.Request) {
	start := 0
	if v := r.URL.Query().Get("start"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			start = n
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.taskByUPID(w, r)
	if t == nil {
		return
	}
	items := []j{}
	for i, line := range t.log {
		if i < start {
			continue
		}
		items = append(items, j{"n": i + 1, "t": line})
	}
	writeData(w, items)
}

func (s *Server) postVzdump(w http.ResponseWriter, r *http.Request) {
	nodeName := chi.URLParam(r, "node")
	body, err := parseBody(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "unable to parse request body: %v", err)
		return
	}
	vmid := body["vmid"]
	if vmid == "" {
		writeJSON(w, http.StatusBadRequest, j{"data": nil, "errors": j{"vmid": "property is missing and it is not optional"}})
		return
	}
	storageName := body["storage"]
	if storageName == "" {
		storageName = "local"
	}
	mode := body["mode"]
	if mode == "" {
		mode = "snapshot"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n := s.nodeByName(nodeName)
	if n == nil {
		writeErr(w, http.StatusInternalServerError, "no such node '%s'", nodeName)
		return
	}
	lines := []string{
		fmt.Sprintf("INFO: starting new backup job: vzdump %s --storage %s --mode %s", vmid, storageName, mode),
		fmt.Sprintf("INFO: Starting Backup of VM %s", vmid),
		fmt.Sprintf("INFO: creating vzdump archive '/mnt/%s/dump/vzdump-%s.vma.zst'", storageName, vmid),
		"INFO: backup is sparse: 21.5 GiB (67%) zero data",
		fmt.Sprintf("INFO: Finished Backup of VM %s (00:00:02)", vmid),
		"INFO: Backup job finished successfully",
	}
	upid := s.startTaskLocked(nodeName, "vzdump", vmid, s.taskDelay+s.taskDelay/2, lines, func() {
		// A backup plausibly consumes some space on the target storage.
		for _, st := range n.storages {
			if st.name == storageName && st.used+gib < st.total {
				st.used += gib
			}
		}
	})
	writeData(w, upid)
}
