package pvesim

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

func (s *Server) getGuestStatus(w http.ResponseWriter, r *http.Request) {
	kind, nodeName, vmid := chi.URLParam(r, "kind"), chi.URLParam(r, "node"), pathVmid(r)
	s.mu.Lock()
	defer s.mu.Unlock()
	g, err := s.findGuest(kind, nodeName, vmid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "%v", err)
		return
	}
	cpu, mem, disk, uptime := s.guestMetrics(g)
	resp := j{
		"status": g.status, "vmid": g.vmid, "name": g.name,
		"cpu": cpu, "cpus": g.cores, "mem": mem, "maxmem": g.maxmem,
		"disk": disk, "maxdisk": g.maxdisk, "uptime": uptime,
		"ha": j{"managed": 0},
	}
	if g.kind == "qemu" {
		qmp := g.status
		if g.status == "paused" {
			qmp = "paused"
		}
		resp["qmpstatus"] = qmp
		resp["agent"] = 1
	}
	writeData(w, resp)
}

func (s *Server) postGuestAction(w http.ResponseWriter, r *http.Request) {
	kind, nodeName, vmid := chi.URLParam(r, "kind"), chi.URLParam(r, "node"), pathVmid(r)
	action := chi.URLParam(r, "action")
	label := guestLabel(kind)

	s.mu.Lock()
	g, err := s.findGuest(kind, nodeName, vmid)
	if err != nil {
		s.mu.Unlock()
		writeErr(w, http.StatusInternalServerError, "%v", err)
		return
	}
	var apply func()
	switch action {
	case "start":
		if g.template {
			err = fmt.Errorf("you can't start a template")
		} else if g.status != "stopped" {
			err = fmt.Errorf("%s %d already running", label, vmid)
		}
		apply = func() { g.status = "running"; g.started = time.Now() }
	case "stop":
		if g.status == "stopped" {
			err = fmt.Errorf("%s %d not running", label, vmid)
		}
		apply = func() { g.status = "stopped" }
	case "shutdown":
		if g.status != "running" {
			err = fmt.Errorf("%s %d not running", label, vmid)
		}
		apply = func() { g.status = "stopped" }
	case "reboot":
		if g.status != "running" {
			err = fmt.Errorf("%s %d not running", label, vmid)
		}
		apply = func() { g.started = time.Now() }
	case "suspend":
		if g.status != "running" {
			err = fmt.Errorf("%s %d not running", label, vmid)
		}
		apply = func() { g.status = "paused" }
	case "resume":
		if g.status != "paused" {
			err = fmt.Errorf("%s %d not suspended", label, vmid)
		}
		apply = func() { g.status = "running" }
	default:
		s.mu.Unlock()
		writeJSON(w, http.StatusNotImplemented, j{"errors": j{"path": "not implemented in pvesim"}})
		return
	}
	if err != nil {
		s.mu.Unlock()
		writeErr(w, http.StatusInternalServerError, "%v", err)
		return
	}
	ttype := taskPrefix(kind) + action
	lines := []string{fmt.Sprintf("%s %s %d", action, label, vmid)}
	upid := s.startTaskLocked(nodeName, ttype, strconv.Itoa(vmid), s.taskDelay, lines, apply)
	s.mu.Unlock()
	writeData(w, upid)
}

func (s *Server) getGuestConfig(w http.ResponseWriter, r *http.Request) {
	kind, nodeName, vmid := chi.URLParam(r, "kind"), chi.URLParam(r, "node"), pathVmid(r)
	s.mu.Lock()
	defer s.mu.Unlock()
	g, err := s.findGuest(kind, nodeName, vmid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "%v", err)
		return
	}
	cfg := j{}
	for k, v := range g.config {
		cfg[k] = v
	}
	writeData(w, cfg)
}

func (s *Server) setGuestConfig(w http.ResponseWriter, r *http.Request) {
	kind, nodeName, vmid := chi.URLParam(r, "kind"), chi.URLParam(r, "node"), pathVmid(r)
	body, err := parseBody(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "unable to parse request body: %v", err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ferr := s.findGuest(kind, nodeName, vmid)
	if ferr != nil {
		writeErr(w, http.StatusInternalServerError, "%v", ferr)
		return
	}
	if del, ok := body["delete"]; ok {
		for _, k := range splitList(del) {
			delete(g.config, k)
		}
		delete(body, "delete")
	}
	delete(body, "digest")
	for k, v := range body {
		g.config[k] = v
		// Keep derived state coherent with the config keys clients change.
		switch k {
		case "name", "hostname":
			g.name = v
		case "cores":
			if n, err := strconv.Atoi(v); err == nil {
				g.cores = n
			}
		case "memory":
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				g.maxmem = n * (1 << 20)
			}
		case "tags":
			g.tags = v
		}
	}
	writeData(w, nil)
}

// splitList splits PVE's comma- or semicolon-separated list values.
func splitList(v string) []string {
	var out []string
	field := ""
	for _, c := range v {
		if c == ',' || c == ';' {
			if field != "" {
				out = append(out, field)
			}
			field = ""
			continue
		}
		field += string(c)
	}
	if field != "" {
		out = append(out, field)
	}
	return out
}

// ---- snapshots -------------------------------------------------------------

func (s *Server) getSnapshots(w http.ResponseWriter, r *http.Request) {
	kind, nodeName, vmid := chi.URLParam(r, "kind"), chi.URLParam(r, "node"), pathVmid(r)
	s.mu.Lock()
	defer s.mu.Unlock()
	g, err := s.findGuest(kind, nodeName, vmid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "%v", err)
		return
	}
	items := make([]j, 0, len(g.snaps)+1)
	parent := ""
	for _, sn := range g.snaps {
		item := j{"name": sn.name, "description": sn.desc, "snaptime": sn.taken, "vmstate": sn.vmstate}
		if parent != "" {
			item["parent"] = parent
		}
		parent = sn.name
		items = append(items, item)
	}
	cur := j{"name": "current", "description": "You are here!"}
	if parent != "" {
		cur["parent"] = parent
	}
	items = append(items, cur)
	writeData(w, items)
}

func (s *Server) postSnapshot(w http.ResponseWriter, r *http.Request) {
	kind, nodeName, vmid := chi.URLParam(r, "kind"), chi.URLParam(r, "node"), pathVmid(r)
	body, err := parseBody(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "unable to parse request body: %v", err)
		return
	}
	name := body["snapname"]
	if name == "" {
		writeJSON(w, http.StatusBadRequest, j{"data": nil, "errors": j{"snapname": "property is missing and it is not optional"}})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ferr := s.findGuest(kind, nodeName, vmid)
	if ferr != nil {
		writeErr(w, http.StatusInternalServerError, "%v", ferr)
		return
	}
	if g.findSnap(name) != nil {
		writeErr(w, http.StatusInternalServerError, "snapshot name '%s' already used", name)
		return
	}
	vmstate := 0
	if body["vmstate"] == "1" {
		vmstate = 1
	}
	desc := body["description"]
	lines := []string{fmt.Sprintf("snapshotting %s %d as '%s'", guestLabel(kind), vmid, name)}
	upid := s.startTaskLocked(nodeName, taskPrefix(kind)+"snapshot", strconv.Itoa(vmid), s.taskDelay, lines, func() {
		g.snaps = append(g.snaps, &snapshot{name: name, desc: desc, taken: time.Now().Unix(), vmstate: vmstate})
	})
	writeData(w, upid)
}

func (s *Server) postSnapshotRollback(w http.ResponseWriter, r *http.Request) {
	kind, nodeName, vmid := chi.URLParam(r, "kind"), chi.URLParam(r, "node"), pathVmid(r)
	name := chi.URLParam(r, "name")
	s.mu.Lock()
	defer s.mu.Unlock()
	g, err := s.findGuest(kind, nodeName, vmid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if g.findSnap(name) == nil {
		writeErr(w, http.StatusInternalServerError, "snapshot '%s' does not exist", name)
		return
	}
	lines := []string{fmt.Sprintf("rolling back %s %d to snapshot '%s'", guestLabel(kind), vmid, name)}
	upid := s.startTaskLocked(nodeName, taskPrefix(kind)+"rollback", strconv.Itoa(vmid), s.taskDelay, lines, nil)
	writeData(w, upid)
}

func (s *Server) deleteSnapshot(w http.ResponseWriter, r *http.Request) {
	kind, nodeName, vmid := chi.URLParam(r, "kind"), chi.URLParam(r, "node"), pathVmid(r)
	name := chi.URLParam(r, "name")
	s.mu.Lock()
	defer s.mu.Unlock()
	g, err := s.findGuest(kind, nodeName, vmid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if g.findSnap(name) == nil {
		writeErr(w, http.StatusInternalServerError, "snapshot '%s' does not exist", name)
		return
	}
	lines := []string{fmt.Sprintf("deleting snapshot '%s' of %s %d", name, guestLabel(kind), vmid)}
	upid := s.startTaskLocked(nodeName, taskPrefix(kind)+"delsnapshot", strconv.Itoa(vmid), s.taskDelay, lines, func() {
		for i, sn := range g.snaps {
			if sn.name == name {
				g.snaps = append(g.snaps[:i], g.snaps[i+1:]...)
				return
			}
		}
	})
	writeData(w, upid)
}

func (g *guest) findSnap(name string) *snapshot {
	for _, sn := range g.snaps {
		if sn.name == name {
			return sn
		}
	}
	return nil
}

// ---- clone / migrate / delete ----------------------------------------------

func (s *Server) postClone(w http.ResponseWriter, r *http.Request) {
	kind, nodeName, vmid := chi.URLParam(r, "kind"), chi.URLParam(r, "node"), pathVmid(r)
	body, err := parseBody(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "unable to parse request body: %v", err)
		return
	}
	newid, err := strconv.Atoi(body["newid"])
	if err != nil || newid < 100 {
		writeJSON(w, http.StatusBadRequest, j{"data": nil, "errors": j{"newid": "invalid or missing vmid"}})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ferr := s.findGuest(kind, nodeName, vmid)
	if ferr != nil {
		writeErr(w, http.StatusInternalServerError, "%v", ferr)
		return
	}
	if _, exists := s.guests[newid]; exists {
		writeErr(w, http.StatusInternalServerError, "unable to create %s %d: config file already exists", guestLabel(kind), newid)
		return
	}
	target := body["target"]
	if target == "" {
		target = g.node
	}
	if s.nodeByName(target) == nil {
		writeErr(w, http.StatusInternalServerError, "no such node '%s'", target)
		return
	}
	name := body["name"]
	if name == "" {
		name = g.name + "-clone"
	}
	lines := []string{
		fmt.Sprintf("create full clone of drive scsi0 (%v)", g.config["scsi0"]),
		fmt.Sprintf("clone %s %d to %d on node '%s'", guestLabel(kind), vmid, newid, target),
	}
	upid := s.startTaskLocked(nodeName, taskPrefix(kind)+"clone", strconv.Itoa(vmid), s.taskDelay, lines, func() {
		if _, exists := s.guests[newid]; exists {
			return // raced with another create; keep the sim consistent
		}
		clone := &guest{
			vmid: newid, kind: g.kind, name: name, node: target,
			status: "stopped", cores: g.cores, maxmem: g.maxmem, maxdisk: g.maxdisk,
			config: map[string]any{},
		}
		for k, v := range g.config {
			clone.config[k] = v
		}
		delete(clone.config, "template")
		if g.kind == "qemu" {
			clone.config["name"] = name
			clone.config["scsi0"] = fmt.Sprintf("tank:vm-%d-disk-0,size=%dG", newid, g.maxdisk/gib)
			clone.config["net0"] = "virtio=" + s.mac() + ",bridge=vmbr0"
		} else {
			clone.config["hostname"] = name
			clone.config["rootfs"] = fmt.Sprintf("tank:subvol-%d-disk-0,size=%dG", newid, g.maxdisk/gib)
			clone.config["net0"] = "name=eth0,bridge=vmbr0,ip=dhcp,hwaddr=" + s.mac()
		}
		s.guests[newid] = clone
	})
	writeData(w, upid)
}

func (s *Server) postMigrate(w http.ResponseWriter, r *http.Request) {
	kind, nodeName, vmid := chi.URLParam(r, "kind"), chi.URLParam(r, "node"), pathVmid(r)
	body, err := parseBody(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "unable to parse request body: %v", err)
		return
	}
	target := body["target"]
	if target == "" {
		writeJSON(w, http.StatusBadRequest, j{"data": nil, "errors": j{"target": "property is missing and it is not optional"}})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ferr := s.findGuest(kind, nodeName, vmid)
	if ferr != nil {
		writeErr(w, http.StatusInternalServerError, "%v", ferr)
		return
	}
	if s.nodeByName(target) == nil {
		writeErr(w, http.StatusInternalServerError, "no such node '%s'", target)
		return
	}
	if target == g.node {
		writeErr(w, http.StatusInternalServerError, "%s %d is already on node '%s'", guestLabel(kind), vmid, target)
		return
	}
	ttype := "qmigrate"
	if kind == "lxc" {
		ttype = "vzmigrate"
	}
	lines := []string{
		fmt.Sprintf("starting migration of %s %d to node '%s'", guestLabel(kind), vmid, target),
		"migration finished successfully",
	}
	upid := s.startTaskLocked(nodeName, ttype, strconv.Itoa(vmid), 2*s.taskDelay, lines, func() {
		g.node = target
	})
	writeData(w, upid)
}

func (s *Server) deleteGuest(w http.ResponseWriter, r *http.Request) {
	kind, nodeName, vmid := chi.URLParam(r, "kind"), chi.URLParam(r, "node"), pathVmid(r)
	s.mu.Lock()
	defer s.mu.Unlock()
	g, err := s.findGuest(kind, nodeName, vmid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if g.status != "stopped" {
		writeErr(w, http.StatusInternalServerError, "%s %d is running - destroy failed", guestLabel(kind), vmid)
		return
	}
	lines := []string{fmt.Sprintf("destroy %s %d", guestLabel(kind), vmid)}
	if r.URL.Query().Get("purge") == "1" {
		lines = append(lines, "purging backup jobs and HA resources")
	}
	upid := s.startTaskLocked(nodeName, taskPrefix(kind)+"destroy", strconv.Itoa(vmid), s.taskDelay, lines, func() {
		delete(s.guests, vmid)
	})
	writeData(w, upid)
}

// taskPrefix maps a guest kind to PVE's task-type prefix (qmstart, vzstart, …).
func taskPrefix(kind string) string {
	if kind == "qemu" {
		return "qm"
	}
	return "vz"
}
