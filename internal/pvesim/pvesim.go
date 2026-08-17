// Package pvesim is an in-memory Proxmox VE 8.x API simulator for development
// and end-to-end tests. It serves the subset of /api2/json that HomeHub talks
// to, with realistic wire shapes and asynchronous, task-driven state
// transitions so task polling is observable.
package pvesim

import (
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const gib = int64(1) << 30

// Options configures the simulator; the zero value yields sensible defaults.
type Options struct {
	// QemuCount and LxcCount control how many guests are seeded
	// (defaults 6 and 3). The first QEMU guest is a template.
	QemuCount int
	LxcCount  int
	// TaskDelay is roughly how long async operations take to finish
	// (default 1s; migrations take double). Tests may lower it.
	TaskDelay time.Duration
	// Seed makes load jitter and generated ids deterministic when non-zero.
	Seed uint64
}

// Server holds the simulated cluster state. Create with New; the handler and
// all exported methods are safe for concurrent use.
type Server struct {
	token     string
	taskDelay time.Duration

	mu      sync.Mutex
	rng     *rand.Rand
	nodes   []*node
	guests  map[int]*guest
	tasks   []*task // newest first
	byUPID  map[string]*task
	backups []*backupJob
	tickets map[string]*consoleTicket
	pstart  int
}

type node struct {
	name     string
	ip       string
	maxcpu   int
	maxmem   int64
	booted   time.Time
	storages []*storage
}

type storage struct {
	name    string
	kind    string // "dir" | "zfspool"
	content string
	used    int64
	total   int64
}

type guest struct {
	vmid     int
	kind     string // "qemu" | "lxc"
	name     string
	node     string
	status   string // "stopped" | "running" | "paused"
	template bool
	tags     string
	cores    int
	maxmem   int64 // bytes
	maxdisk  int64 // bytes
	started  time.Time
	config   map[string]any
	snaps    []*snapshot
}

type snapshot struct {
	name    string
	desc    string
	taken   int64
	vmstate int
}

type backupJob struct {
	id     string
	fields map[string]string
}

type consoleTicket struct {
	user   string
	issued time.Time
	used   bool
}

// New builds a simulator seeded with a two-node cluster ("pve1", "pve2") and
// a plausible set of guests and storages.
func New(opts Options) *Server {
	if opts.QemuCount <= 0 {
		opts.QemuCount = 6
	}
	if opts.LxcCount <= 0 {
		opts.LxcCount = 3
	}
	if opts.TaskDelay <= 0 {
		opts.TaskDelay = time.Second
	}
	seed := opts.Seed
	if seed == 0 {
		seed = 0x9e3779b97f4a7c15
	}
	s := &Server{
		token:     "root@pam!homehub=" + uuid.NewString(),
		taskDelay: opts.TaskDelay,
		rng:       rand.New(rand.NewPCG(seed, seed^0xda3e39cb94b95bdb)),
		guests:    map[int]*guest{},
		byUPID:    map[string]*task{},
		tickets:   map[string]*consoleTicket{},
	}
	s.seedState(opts)
	return s
}

// Token returns the full API token ("user@realm!id=secret") the simulator
// accepts via "Authorization: PVEAPIToken=<token>".
func (s *Server) Token() string { return s.token }

type seedGuest struct {
	name, node, status, tags string
	template                 bool
	cores                    int
	memMiB, diskGiB          int64
}

func (s *Server) seedState(opts Options) {
	now := time.Now()
	s.nodes = []*node{
		{
			name: "pve1", ip: "192.168.1.10", maxcpu: 8, maxmem: 32 * gib,
			booted: now.Add(-21 * 24 * time.Hour),
			storages: []*storage{
				{name: "local", kind: "dir", content: "iso,vztmpl,backup", used: 31 * gib, total: 98 * gib},
				{name: "tank", kind: "zfspool", content: "images,rootdir", used: 812 * gib, total: 1863 * gib},
			},
		},
		{
			name: "pve2", ip: "192.168.1.11", maxcpu: 16, maxmem: 64 * gib,
			booted: now.Add(-9 * 24 * time.Hour),
			storages: []*storage{
				{name: "local", kind: "dir", content: "iso,vztmpl,backup", used: 22 * gib, total: 98 * gib},
				{name: "tank", kind: "zfspool", content: "images,rootdir", used: 1204 * gib, total: 3726 * gib},
			},
		},
	}

	qemu := []seedGuest{
		{name: "debian12-tmpl", node: "pve1", status: "stopped", template: true, cores: 2, memMiB: 2048, diskGiB: 32},
		{name: "web-01", node: "pve1", status: "running", tags: "prod;web", cores: 2, memMiB: 4096, diskGiB: 32},
		{name: "db-01", node: "pve1", status: "running", tags: "prod", cores: 4, memMiB: 8192, diskGiB: 64},
		{name: "k8s-master", node: "pve2", status: "running", cores: 4, memMiB: 8192, diskGiB: 64},
		{name: "k8s-worker-1", node: "pve2", status: "running", cores: 8, memMiB: 16384, diskGiB: 128},
		{name: "win11-play", node: "pve2", status: "stopped", cores: 8, memMiB: 16384, diskGiB: 128},
	}
	lxc := []seedGuest{
		{name: "proxy", node: "pve1", status: "running", cores: 1, memMiB: 512, diskGiB: 8},
		{name: "pihole", node: "pve2", status: "running", tags: "dns", cores: 1, memMiB: 512, diskGiB: 8},
		{name: "media-tools", node: "pve1", status: "stopped", cores: 2, memMiB: 1024, diskGiB: 16},
	}
	for i := 0; i < opts.QemuCount; i++ {
		sg := seedGuest{name: fmt.Sprintf("vm-%d", 100+i), node: s.nodes[i%2].name, status: "stopped", cores: 2, memMiB: 2048, diskGiB: 32}
		if i < len(qemu) {
			sg = qemu[i]
		}
		s.addSeedGuest("qemu", 100+i, sg)
	}
	for i := 0; i < opts.LxcCount; i++ {
		sg := seedGuest{name: fmt.Sprintf("ct-%d", 200+i), node: s.nodes[i%2].name, status: "stopped", cores: 1, memMiB: 512, diskGiB: 8}
		if i < len(lxc) {
			sg = lxc[i]
		}
		s.addSeedGuest("lxc", 200+i, sg)
	}
	// A pre-existing snapshot so the snapshot list is non-trivial out of the box.
	if g, ok := s.guests[101]; ok {
		g.snaps = append(g.snaps, &snapshot{
			name: "pre-upgrade", desc: "before nginx 1.26", taken: now.Add(-72 * time.Hour).Unix(),
		})
	}

	s.backups = []*backupJob{{
		id: "backup-7f3a2b10-1cd4",
		fields: map[string]string{
			"schedule": "02:30", "storage": "tank", "mode": "snapshot",
			"enabled": "1", "vmid": "101,102", "compress": "zstd",
		},
	}}
}

func (s *Server) addSeedGuest(kind string, vmid int, sg seedGuest) {
	g := &guest{
		vmid:     vmid,
		kind:     kind,
		name:     sg.name,
		node:     sg.node,
		status:   sg.status,
		template: sg.template,
		tags:     sg.tags,
		cores:    sg.cores,
		maxmem:   sg.memMiB * (1 << 20),
		maxdisk:  sg.diskGiB * gib,
	}
	if g.status == "running" {
		g.started = time.Now().Add(-time.Duration(6+s.rng.IntN(90)) * time.Hour)
	}
	if kind == "qemu" {
		ostype := "l26"
		if strings.HasPrefix(sg.name, "win") {
			ostype = "win11"
		}
		g.config = map[string]any{
			"name":    sg.name,
			"cores":   sg.cores,
			"sockets": 1,
			"memory":  strconv.FormatInt(sg.memMiB, 10),
			"ostype":  ostype,
			"boot":    "order=scsi0;net0",
			"scsihw":  "virtio-scsi-single",
			"scsi0":   fmt.Sprintf("tank:vm-%d-disk-0,size=%dG", vmid, sg.diskGiB),
			"net0":    "virtio=" + s.mac() + ",bridge=vmbr0",
			"onboot":  1,
			"agent":   "1",
		}
		if sg.template {
			g.config["template"] = 1
		}
	} else {
		g.config = map[string]any{
			"hostname":     sg.name,
			"cores":        sg.cores,
			"memory":       strconv.FormatInt(sg.memMiB, 10),
			"swap":         512,
			"rootfs":       fmt.Sprintf("tank:subvol-%d-disk-0,size=%dG", vmid, sg.diskGiB),
			"net0":         "name=eth0,bridge=vmbr0,ip=dhcp,hwaddr=" + s.mac(),
			"ostype":       "debian",
			"unprivileged": 1,
		}
	}
	if sg.tags != "" {
		g.config["tags"] = sg.tags
	}
	s.guests[vmid] = g
}

// mac generates a MAC in Proxmox's BC:24:11 prefix. Caller holds s.mu (or is
// still inside New).
func (s *Server) mac() string {
	return fmt.Sprintf("BC:24:11:%02X:%02X:%02X", s.rng.IntN(256), s.rng.IntN(256), s.rng.IntN(256))
}

// ---- lookup and metrics helpers (callers hold s.mu) ------------------------

func (s *Server) nodeByName(name string) *node {
	for _, n := range s.nodes {
		if n.name == name {
			return n
		}
	}
	return nil
}

func confDir(kind string) string {
	if kind == "qemu" {
		return "qemu-server"
	}
	return "lxc"
}

// guestLabel is how PVE error strings name a guest kind.
func guestLabel(kind string) string {
	if kind == "qemu" {
		return "VM"
	}
	return "CT"
}

func (s *Server) findGuest(kind, nodeName string, vmid int) (*guest, error) {
	g, ok := s.guests[vmid]
	if !ok || g.kind != kind || g.node != nodeName {
		return nil, fmt.Errorf("Configuration file 'nodes/%s/%s/%d.conf' does not exist", nodeName, confDir(kind), vmid)
	}
	return g, nil
}

// jitter returns base scaled by a random factor in [1-spread, 1+spread].
func (s *Server) jitter(base, spread float64) float64 {
	return base * (1 + spread*(2*s.rng.Float64()-1))
}

func (s *Server) guestMetrics(g *guest) (cpu float64, mem, disk, uptime int64) {
	switch g.status {
	case "running":
		cpu = 0.01 + s.rng.Float64()*0.2
		mem = int64(float64(g.maxmem) * (0.45 + s.rng.Float64()*0.3))
		uptime = int64(time.Since(g.started).Seconds())
	case "paused":
		mem = int64(float64(g.maxmem) * 0.5)
		uptime = int64(time.Since(g.started).Seconds())
	}
	if g.kind == "lxc" && g.status == "running" {
		disk = int64(float64(g.maxdisk) * 0.3)
	}
	return cpu, mem, disk, uptime
}

func (s *Server) nodeMetrics(n *node) (cpu float64, mem, disk, maxdisk, uptime int64) {
	cpu = 0.02 + s.rng.Float64()*0.15
	mem = int64(s.jitter(float64(n.maxmem)*0.4, 0.2))
	local := n.storages[0]
	return cpu, mem, local.used, local.total, int64(time.Since(n.booted).Seconds())
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
