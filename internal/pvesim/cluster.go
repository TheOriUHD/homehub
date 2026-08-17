package pvesim

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

func (s *Server) getVersion(w http.ResponseWriter, _ *http.Request) {
	writeData(w, j{"version": "8.4.1", "release": "8.4", "repoid": "2a56901abf1c9b3f"})
}

func (s *Server) getClusterStatus(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := []j{{
		"type": "cluster", "id": "cluster", "name": "homelab",
		"quorate": 1, "version": 2, "nodes": len(s.nodes),
	}}
	for i, n := range s.nodes {
		items = append(items, j{
			"type": "node", "id": "node/" + n.name, "name": n.name,
			"nodeid": i + 1, "online": 1, "local": boolToInt(i == 0),
			"ip": n.ip, "level": "",
		})
	}
	writeData(w, items)
}

// sortedGuests returns guests ordered by vmid for stable output.
func (s *Server) sortedGuests() []*guest {
	out := make([]*guest, 0, len(s.guests))
	for _, g := range s.guests {
		out = append(out, g)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].vmid < out[b].vmid })
	return out
}

func (s *Server) guestResource(g *guest) j {
	cpu, mem, disk, uptime := s.guestMetrics(g)
	res := j{
		"id":       g.kind + "/" + strconv.Itoa(g.vmid),
		"type":     g.kind,
		"vmid":     g.vmid,
		"name":     g.name,
		"node":     g.node,
		"status":   g.status,
		"cpu":      cpu,
		"maxcpu":   g.cores,
		"mem":      mem,
		"maxmem":   g.maxmem,
		"disk":     disk,
		"maxdisk":  g.maxdisk,
		"uptime":   uptime,
		"template": boolToInt(g.template),
	}
	if g.tags != "" {
		res["tags"] = g.tags
	}
	return res
}

func (s *Server) getClusterResources(w http.ResponseWriter, r *http.Request) {
	typ := r.URL.Query().Get("type")
	s.mu.Lock()
	defer s.mu.Unlock()
	items := []j{}
	if typ == "" || typ == "node" {
		for _, n := range s.nodes {
			cpu, mem, disk, maxdisk, uptime := s.nodeMetrics(n)
			items = append(items, j{
				"id": "node/" + n.name, "type": "node", "node": n.name,
				"status": "online", "cpu": cpu, "maxcpu": n.maxcpu,
				"mem": mem, "maxmem": n.maxmem, "disk": disk, "maxdisk": maxdisk,
				"uptime": uptime, "level": "",
			})
		}
	}
	if typ == "" || typ == "vm" {
		for _, g := range s.sortedGuests() {
			items = append(items, s.guestResource(g))
		}
	}
	if typ == "" || typ == "storage" {
		for _, n := range s.nodes {
			for _, st := range n.storages {
				items = append(items, j{
					"id":      fmt.Sprintf("storage/%s/%s", n.name, st.name),
					"type":    "storage",
					"storage": st.name,
					"node":    n.name,
					"status":  "available",
					"disk":    st.used,
					"maxdisk": st.total,
					"plugintype": st.kind,
					"content": st.content,
					"shared":  0,
				})
			}
		}
	}
	writeData(w, items)
}

func (s *Server) getNodes(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := []j{}
	for _, n := range s.nodes {
		cpu, mem, _, maxdisk, uptime := s.nodeMetrics(n)
		items = append(items, j{
			"node": n.name, "id": "node/" + n.name, "type": "node",
			"status": "online", "cpu": cpu, "maxcpu": n.maxcpu,
			"mem": mem, "maxmem": n.maxmem, "maxdisk": maxdisk,
			"uptime": uptime, "level": "",
		})
	}
	writeData(w, items)
}

func (s *Server) getNodeStatus(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "node")
	s.mu.Lock()
	defer s.mu.Unlock()
	n := s.nodeByName(name)
	if n == nil {
		writeErr(w, http.StatusInternalServerError, "no such node '%s'", name)
		return
	}
	cpu, mem, _, _, uptime := s.nodeMetrics(n)
	load := cpu * float64(n.maxcpu)
	writeData(w, j{
		"cpu": cpu,
		"loadavg": []string{
			fmt.Sprintf("%.2f", s.jitter(load, 0.3)),
			fmt.Sprintf("%.2f", s.jitter(load, 0.2)),
			fmt.Sprintf("%.2f", s.jitter(load, 0.1)),
		},
		"memory":     j{"used": mem, "total": n.maxmem, "free": n.maxmem - mem},
		"uptime":     uptime,
		"pveversion": "pve-manager/8.4.1",
		"kversion":   "Linux 6.8.12-9-pve #1 SMP PREEMPT_DYNAMIC PMX 6.8.12-9 (2025-03-16T19:18Z)",
		"cpuinfo":    j{"cpus": n.maxcpu, "model": "AMD Ryzen 7 5700G with Radeon Graphics", "sockets": 1},
	})
}

func (s *Server) getNodeStorage(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "node")
	s.mu.Lock()
	defer s.mu.Unlock()
	n := s.nodeByName(name)
	if n == nil {
		writeErr(w, http.StatusInternalServerError, "no such node '%s'", name)
		return
	}
	items := []j{}
	for _, st := range n.storages {
		items = append(items, j{
			"storage":       st.name,
			"type":          st.kind,
			"content":       st.content,
			"active":        1,
			"enabled":       1,
			"used":          st.used,
			"total":         st.total,
			"avail":         st.total - st.used,
			"used_fraction": float64(st.used) / float64(st.total),
			"shared":        0,
		})
	}
	writeData(w, items)
}

func (s *Server) getNodeGuests(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "node")
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.nodeByName(name) == nil {
			writeErr(w, http.StatusInternalServerError, "no such node '%s'", name)
			return
		}
		items := []j{}
		for _, g := range s.sortedGuests() {
			if g.kind == kind && g.node == name {
				items = append(items, s.guestResource(g))
			}
		}
		writeData(w, items)
	}
}

// ---- backup jobs -----------------------------------------------------------

func (s *Server) getBackupJobs(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := []j{}
	for _, b := range s.backups {
		item := j{"id": b.id, "type": "vzdump", "next-run": time.Now().Add(24 * time.Hour).Unix()}
		for k, v := range b.fields {
			if k == "enabled" {
				if n, err := strconv.Atoi(v); err == nil {
					item[k] = n
					continue
				}
			}
			item[k] = v
		}
		items = append(items, item)
	}
	writeData(w, items)
}

func (s *Server) postBackupJob(w http.ResponseWriter, r *http.Request) {
	body, err := parseBody(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "unable to parse request body: %v", err)
		return
	}
	if body["vmid"] == "" && body["all"] == "" {
		writeJSON(w, http.StatusBadRequest, j{"data": nil, "errors": j{"vmid": "property is missing and it is not optional"}})
		return
	}
	if body["enabled"] == "" {
		body["enabled"] = "1"
	}
	s.mu.Lock()
	id := fmt.Sprintf("backup-%08x-%04x", s.rng.Uint32(), s.rng.Uint32()&0xffff)
	s.backups = append(s.backups, &backupJob{id: id, fields: body})
	s.mu.Unlock()
	writeData(w, nil)
}

func (s *Server) putBackupJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	body, err := parseBody(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "unable to parse request body: %v", err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, b := range s.backups {
		if b.id == id {
			for k, v := range body {
				b.fields[k] = v
			}
			writeData(w, nil)
			return
		}
	}
	writeErr(w, http.StatusInternalServerError, "backup job '%s' does not exist", id)
}
