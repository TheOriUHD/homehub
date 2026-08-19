// Package netscan is HomeHub's built-in IP scanner: sweep a range, report
// which hosts are alive, their hostname and MAC, and which ports are open —
// an Angry-IP-Scanner-style tool that runs from the HomeHub server (which
// sits on the LAN). It reuses internal/nettools for ICMP and TCP probing.
package netscan

import (
	"bufio"
	"context"
	"encoding/binary"
	"net"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/TheOriUHD/homehub/internal/nettools"
)

// DefaultPorts is a curated common-ports set (standard services plus the
// homelab suspects: Proxmox 8006, Home Assistant 8123, Plex 32400, …).
var DefaultPorts = []int{
	20, 21, 22, 23, 25, 53, 80, 110, 111, 135, 139, 143, 161, 389, 443, 445,
	587, 631, 993, 995, 1433, 1723, 2049, 3000, 3306, 3389, 5000, 5432, 5900,
	6379, 8006, 8080, 8096, 8123, 8443, 9000, 9090, 32400, 51820,
}

// maxHosts bounds one scan so a wide mask can't ask for a million probes.
const maxHosts = 65536

// Options configures a scan.
type Options struct {
	Start       net.IP
	End         net.IP
	Ports       []int
	Concurrency int // hosts probed in parallel; 0 → default
}

// HostResult is one scanned address.
type HostResult struct {
	IP        string  `json:"ip"`
	Hostname  string  `json:"hostname,omitempty"`
	MAC       string  `json:"mac,omitempty"`
	Vendor    string  `json:"vendor,omitempty"`
	Alive     bool    `json:"alive"`
	RttMs     float64 `json:"rttMs,omitempty"`
	OpenPorts []int   `json:"openPorts"`
}

// EnumerateCount returns how many addresses [start,end] covers, or 0 if the
// range is invalid / reversed / too large.
func EnumerateCount(start, end net.IP) int {
	s, e := start.To4(), end.To4()
	if s == nil || e == nil {
		return 0
	}
	su, eu := binary.BigEndian.Uint32(s), binary.BigEndian.Uint32(e)
	if eu < su {
		return 0
	}
	n := int(eu-su) + 1
	if n > maxHosts {
		return 0
	}
	return n
}

func enumerate(start, end net.IP) []net.IP {
	s, e := start.To4(), end.To4()
	su, eu := binary.BigEndian.Uint32(s), binary.BigEndian.Uint32(e)
	out := make([]net.IP, 0, eu-su+1)
	for u := su; ; u++ {
		ip := make(net.IP, 4)
		binary.BigEndian.PutUint32(ip, u)
		out = append(out, ip)
		if u == eu {
			break
		}
	}
	return out
}

// Scan sweeps opts.Start..opts.End, calling emit once per host as it
// completes (from many goroutines — emit MUST be safe to call concurrently).
// It returns when every host is done or ctx is cancelled.
func Scan(ctx context.Context, opts Options, emit func(HostResult)) {
	ports := opts.Ports
	if len(ports) == 0 {
		ports = DefaultPorts
	}
	conc := opts.Concurrency
	if conc <= 0 {
		conc = 48
	}
	ips := enumerate(opts.Start, opts.End)

	sem := make(chan struct{}, conc)
	var wg sync.WaitGroup
	for _, ip := range ips {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(ip net.IP) {
			defer wg.Done()
			defer func() { <-sem }()
			if ctx.Err() != nil {
				return
			}
			emit(scanHost(ctx, ip.String(), ports))
		}(ip)
	}
	wg.Wait()
}

// scanHost probes one address: TCP ports (concurrently) + one ICMP echo in
// parallel, then MAC (ARP) and reverse DNS for hosts that responded.
func scanHost(ctx context.Context, ip string, ports []int) HostResult {
	res := HostResult{IP: ip, OpenPorts: []int{}}

	// Ports, concurrently, each with a short deadline.
	var mu sync.Mutex
	var pwg sync.WaitGroup
	psem := make(chan struct{}, 32)
	for _, p := range ports {
		pwg.Add(1)
		psem <- struct{}{}
		go func(p int) {
			defer pwg.Done()
			defer func() { <-psem }()
			pctx, cancel := context.WithTimeout(ctx, 700*time.Millisecond)
			defer cancel()
			if r := nettools.CheckTCP(pctx, ip, p); r.Open {
				mu.Lock()
				res.OpenPorts = append(res.OpenPorts, p)
				mu.Unlock()
			}
		}(p)
	}

	// ICMP in parallel with the port sweep.
	pingCh := make(chan nettools.PingResult, 1)
	go func() {
		pctx, cancel := context.WithTimeout(ctx, 1200*time.Millisecond)
		defer cancel()
		pingCh <- nettools.Ping(pctx, ip)
	}()

	pwg.Wait()
	ping := <-pingCh
	sort.Ints(res.OpenPorts)
	if ping.Alive {
		res.RttMs = ping.RttMs
	}

	// MAC from the kernel ARP cache (populated by the probes above); only
	// meaningful for hosts on the same L2 segment.
	res.MAC = arpLookup(ip)
	if res.MAC != "" {
		res.Vendor = vendorFor(res.MAC)
	}

	res.Alive = ping.Alive || len(res.OpenPorts) > 0 || res.MAC != ""
	if res.Alive {
		res.Hostname = reverseDNS(ctx, ip)
	}
	return res
}

func reverseDNS(ctx context.Context, ip string) string {
	rctx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
	defer cancel()
	names, err := net.DefaultResolver.LookupAddr(rctx, ip)
	if err != nil || len(names) == 0 {
		return ""
	}
	return strings.TrimSuffix(names[0], ".")
}

// arpLookup reads the kernel ARP table (Linux) for a resolved MAC. Returns
// "" when the entry is missing or incomplete (00:00:00:00:00:00).
func arpLookup(ip string) string {
	f, err := os.Open("/proc/net/arp")
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Scan() // header
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 4 {
			continue
		}
		if fields[0] == ip {
			mac := strings.ToLower(fields[3])
			if mac == "00:00:00:00:00:00" || mac == "" {
				return ""
			}
			return mac
		}
	}
	return ""
}
