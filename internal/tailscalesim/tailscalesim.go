// Package tailscalesim is an in-memory Tailscale cloud API (v2) used to
// verify the tailscale driver without a real tailnet. It mirrors the real
// routing and envelopes — Basic auth with the API key as username, the
// tailnet path segment ("-" or the named tailnet), {"devices":[...]}-style
// wrappers, and {"message":"..."} error bodies with real status codes — so
// path or auth bugs in the driver fail here the way they would in
// production.
package tailscalesim

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	// APIKey carries a parseable key id like real tskey-api tokens.
	APIKey   = "tskey-api-kHOMEHUBSIM1-simsecret0000"
	OwnKeyID = "kHOMEHUBSIM1"
	Tailnet  = "example.com"

	// Fixed device IDs for deterministic tests.
	GatewayID = "50052"
	PhoneID   = "50053"
	NasID     = "50054"
	LaptopID  = "50055"
	PendingID = "50056"
)

type device struct {
	id                string
	name              string
	hostname          string
	user              string
	addresses         []string
	os                string
	clientVersion     string
	updateAvailable   bool
	authorized        bool
	keyExpiryDisabled bool
	online            bool
	created           time.Time
	expires           time.Time
	lastSeen          time.Time
	tags              []string
	advertisedRoutes  []string
	enabledRoutes     []string
}

type key struct {
	id            string
	secret        string
	keyType       string // "auth" or "api"
	description   string
	created       time.Time
	expires       time.Time
	revoked       time.Time
	reusable      bool
	ephemeral     bool
	preauthorized bool
	tags          []string
}

// Sim is a fake tailnet.
type Sim struct {
	mu          sync.Mutex
	devices     map[string]*device
	order       []string
	keys        map[string]*key
	keyOrder    []string
	validTokens map[string]bool // accepted Basic-auth usernames (API tokens)
	nameservers []string
	magicDNS    bool
	searchPaths []string
}

func New() *Sim {
	now := time.Now()
	s := &Sim{
		devices:     map[string]*device{},
		keys:        map[string]*key{},
		nameservers: []string{"1.1.1.1", "9.9.9.9"},
		magicDNS:    true,
		searchPaths: []string{"lan"},
	}
	add := func(d *device) {
		s.devices[d.id] = d
		s.order = append(s.order, d.id)
	}
	add(&device{
		id: GatewayID, name: "gateway.example.com.ts.net", hostname: "gateway",
		user: "theo@example.com", addresses: []string{"100.101.1.1", "fd7a:115c:a1e0::1"},
		os: "linux", clientVersion: "1.86.2", authorized: true, online: true,
		created: now.Add(-400 * 24 * time.Hour), expires: now.Add(120 * 24 * time.Hour),
		lastSeen: now, advertisedRoutes: []string{"192.168.20.0/24", "0.0.0.0/0", "::/0"},
		enabledRoutes: []string{"192.168.20.0/24"},
	})
	add(&device{
		id: PhoneID, name: "phone.example.com.ts.net", hostname: "phone",
		user: "theo@example.com", addresses: []string{"100.101.1.2"},
		os: "iOS", clientVersion: "1.86.0", authorized: true, online: true,
		created: now.Add(-200 * 24 * time.Hour), expires: now.Add(30 * 24 * time.Hour),
		lastSeen: now,
	})
	add(&device{
		id: NasID, name: "nas.example.com.ts.net", hostname: "nas",
		user: "theo@example.com", addresses: []string{"100.101.1.3"},
		os: "linux", clientVersion: "1.80.1", updateAvailable: true, authorized: true,
		keyExpiryDisabled: true,
		created:           now.Add(-300 * 24 * time.Hour), expires: time.Time{},
		lastSeen: now.Add(-36 * time.Hour),
	})
	add(&device{
		id: LaptopID, name: "laptop.example.com.ts.net", hostname: "laptop",
		user: "friend@example.com", addresses: []string{"100.101.1.4"},
		os: "macOS", clientVersion: "1.84.0", updateAvailable: true, authorized: true,
		created: now.Add(-90 * 24 * time.Hour), expires: now.Add(60 * 24 * time.Hour),
		lastSeen: now, online: true, tags: []string{"tag:friend"},
	})
	add(&device{
		id: PendingID, name: "newbox.example.com.ts.net", hostname: "newbox",
		user: "theo@example.com", addresses: []string{"100.101.1.5"},
		os: "linux", clientVersion: "1.86.2",
		created: now.Add(-time.Hour), expires: now.Add(180 * 24 * time.Hour),
		lastSeen: now.Add(-30 * time.Minute),
	})
	s.addKey(&key{
		id: "k1234567CNTRL", secret: "", keyType: "auth", description: "NAS bootstrap",
		created: now.Add(-60 * 24 * time.Hour), expires: now.Add(30 * 24 * time.Hour),
		reusable: true,
	})
	// The sim's own API token, so the auto-renewal flow can read its expiry.
	s.addKey(&key{
		id: OwnKeyID, secret: APIKey, keyType: "api", description: "homehub",
		created: now.Add(-80 * 24 * time.Hour), expires: now.Add(10 * 24 * time.Hour),
	})
	s.validTokens = map[string]bool{APIKey: true}
	return s
}

func (s *Sim) addKey(k *key) {
	s.keys[k.id] = k
	s.keyOrder = append(s.keyOrder, k.id)
}

// ---- HTTP -------------------------------------------------------------------

// SetOwnKeyExpiry moves the sim's own token expiry (for renewal tests).
func (s *Sim) SetOwnKeyExpiry(expires time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if k, ok := s.keys[OwnKeyID]; ok {
		k.expires = expires
	}
}

func (s *Sim) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	user, _, ok := r.BasicAuth()
	s.mu.Lock()
	valid := ok && s.validTokens[user]
	s.mu.Unlock()
	if !valid {
		s.fail(w, http.StatusUnauthorized, "invalid key")
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/v2/")
	if path == r.URL.Path {
		http.NotFound(w, r)
		return
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")

	s.mu.Lock()
	defer s.mu.Unlock()

	switch {
	case parts[0] == "tailnet" && len(parts) >= 3:
		if parts[1] != Tailnet && parts[1] != "-" {
			s.fail(w, http.StatusNotFound, "tailnet not found")
			return
		}
		s.serveTailnet(w, r, parts[2:])
	case parts[0] == "device" && len(parts) >= 2:
		s.serveDevice(w, r, parts[1], parts[2:])
	default:
		http.NotFound(w, r)
	}
}

func (s *Sim) serveTailnet(w http.ResponseWriter, r *http.Request, parts []string) {
	switch {
	case parts[0] == "devices" && len(parts) == 1 && r.Method == http.MethodGet:
		all := r.URL.Query().Get("fields") == "all"
		out := make([]map[string]any, 0, len(s.order))
		for _, id := range s.order {
			out = append(out, s.devicePayload(s.devices[id], all))
		}
		s.reply(w, map[string]any{"devices": out})

	case parts[0] == "keys" && len(parts) == 1 && r.Method == http.MethodGet:
		out := make([]map[string]any, 0, len(s.keyOrder))
		for _, id := range s.keyOrder {
			k := s.keys[id]
			// The real list endpoint returns skeletons.
			out = append(out, map[string]any{"id": k.id, "description": k.description})
		}
		s.reply(w, map[string]any{"keys": out})

	case parts[0] == "keys" && len(parts) == 1 && r.Method == http.MethodPost:
		var req struct {
			Capabilities struct {
				Devices struct {
					Create struct {
						Reusable      bool     `json:"reusable"`
						Ephemeral     bool     `json:"ephemeral"`
						Preauthorized bool     `json:"preauthorized"`
						Tags          []string `json:"tags"`
					} `json:"create"`
				} `json:"devices"`
			} `json:"capabilities"`
			ExpirySeconds int64  `json:"expirySeconds"`
			Description   string `json:"description"`
			KeyType       string `json:"keyType"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.fail(w, http.StatusBadRequest, "bad request body")
			return
		}
		if req.ExpirySeconds <= 0 {
			s.fail(w, http.StatusBadRequest, "expirySeconds must be positive")
			return
		}
		now := time.Now()
		id := "k" + strings.ToUpper(strings.ReplaceAll(uuid.NewString()[:12], "-", ""))
		k := &key{
			id:          id,
			keyType:     "auth",
			secret:      "tskey-auth-" + uuid.NewString(),
			description: req.Description,
			created:     now, expires: now.Add(time.Duration(req.ExpirySeconds) * time.Second),
			reusable: req.Capabilities.Devices.Create.Reusable, ephemeral: req.Capabilities.Devices.Create.Ephemeral,
			preauthorized: req.Capabilities.Devices.Create.Preauthorized, tags: req.Capabilities.Devices.Create.Tags,
		}
		if req.KeyType == "api" {
			k.keyType = "api"
			k.secret = "tskey-api-" + id + "-" + strings.ReplaceAll(uuid.NewString(), "-", "")
			s.validTokens[k.secret] = true
		}
		s.addKey(k)
		s.reply(w, s.keyPayload(k, true))

	case parts[0] == "keys" && len(parts) == 2:
		k, ok := s.keys[parts[1]]
		if !ok {
			s.fail(w, http.StatusNotFound, "key not found")
			return
		}
		switch r.Method {
		case http.MethodGet:
			s.reply(w, s.keyPayload(k, false))
		case http.MethodDelete:
			k.revoked = time.Now()
			if k.keyType == "api" && k.secret != "" {
				delete(s.validTokens, k.secret)
			}
			s.reply(w, nil)
		default:
			http.NotFound(w, r)
		}

	case parts[0] == "dns" && len(parts) == 2:
		s.serveDNS(w, r, parts[1])

	default:
		http.NotFound(w, r)
	}
}

func (s *Sim) serveDNS(w http.ResponseWriter, r *http.Request, leaf string) {
	readBody := func(out any) bool {
		if err := json.NewDecoder(r.Body).Decode(out); err != nil {
			s.fail(w, http.StatusBadRequest, "bad request body")
			return false
		}
		return true
	}
	switch {
	case leaf == "nameservers" && r.Method == http.MethodGet:
		s.reply(w, map[string]any{"dns": s.nameservers})
	case leaf == "nameservers" && r.Method == http.MethodPost:
		var req struct {
			DNS []string `json:"dns"`
		}
		if !readBody(&req) {
			return
		}
		s.nameservers = req.DNS
		if len(s.nameservers) == 0 {
			// Real behavior: removing all nameservers turns MagicDNS off.
			s.magicDNS = false
		}
		s.reply(w, map[string]any{"dns": s.nameservers, "magicDNS": s.magicDNS})
	case leaf == "preferences" && r.Method == http.MethodGet:
		s.reply(w, map[string]any{"magicDNS": s.magicDNS})
	case leaf == "preferences" && r.Method == http.MethodPost:
		var req struct {
			MagicDNS bool `json:"magicDNS"`
		}
		if !readBody(&req) {
			return
		}
		if req.MagicDNS && len(s.nameservers) == 0 {
			s.fail(w, http.StatusBadRequest, "at least one nameserver is required to enable MagicDNS")
			return
		}
		s.magicDNS = req.MagicDNS
		s.reply(w, nil)
	case leaf == "searchpaths" && r.Method == http.MethodGet:
		s.reply(w, map[string]any{"searchPaths": s.searchPaths})
	case leaf == "searchpaths" && r.Method == http.MethodPost:
		var req struct {
			SearchPaths []string `json:"searchPaths"`
		}
		if !readBody(&req) {
			return
		}
		s.searchPaths = req.SearchPaths
		s.reply(w, map[string]any{"searchPaths": s.searchPaths})
	default:
		http.NotFound(w, r)
	}
}

func (s *Sim) serveDevice(w http.ResponseWriter, r *http.Request, id string, parts []string) {
	d, ok := s.devices[id]
	if !ok {
		s.fail(w, http.StatusNotFound, "device not found")
		return
	}
	leaf := ""
	if len(parts) > 0 {
		leaf = parts[0]
	}
	switch {
	case leaf == "" && r.Method == http.MethodGet:
		s.reply(w, s.devicePayload(d, true))
	case leaf == "" && r.Method == http.MethodDelete:
		delete(s.devices, id)
		s.order = slices.DeleteFunc(s.order, func(v string) bool { return v == id })
		s.reply(w, nil)
	case leaf == "authorized" && r.Method == http.MethodPost:
		var req struct {
			Authorized bool `json:"authorized"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.fail(w, http.StatusBadRequest, "bad request body")
			return
		}
		d.authorized = req.Authorized
		s.reply(w, nil)
	case leaf == "routes" && r.Method == http.MethodGet:
		s.reply(w, map[string]any{"advertisedRoutes": d.advertisedRoutes, "enabledRoutes": d.enabledRoutes})
	case leaf == "routes" && r.Method == http.MethodPost:
		var req struct {
			Routes []string `json:"routes"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Routes == nil {
			s.fail(w, http.StatusBadRequest, "routes field is required")
			return
		}
		d.enabledRoutes = req.Routes
		s.reply(w, map[string]any{"advertisedRoutes": d.advertisedRoutes, "enabledRoutes": d.enabledRoutes})
	case leaf == "key" && r.Method == http.MethodPost:
		var req struct {
			KeyExpiryDisabled bool `json:"keyExpiryDisabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.fail(w, http.StatusBadRequest, "bad request body")
			return
		}
		d.keyExpiryDisabled = req.KeyExpiryDisabled
		s.reply(w, nil)
	case leaf == "expire" && r.Method == http.MethodPost:
		d.expires = time.Now()
		d.online = false
		d.lastSeen = time.Now()
		s.reply(w, nil)
	default:
		http.NotFound(w, r)
	}
}

// ---- payloads ---------------------------------------------------------------

func tsTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func (s *Sim) devicePayload(d *device, all bool) map[string]any {
	out := map[string]any{
		"id":                        d.id,
		"nodeId":                    "n" + d.id,
		"name":                      d.name,
		"hostname":                  d.hostname,
		"user":                      d.user,
		"addresses":                 d.addresses,
		"os":                        d.os,
		"clientVersion":             d.clientVersion,
		"updateAvailable":           d.updateAvailable,
		"authorized":                d.authorized,
		"isExternal":                false,
		"keyExpiryDisabled":         d.keyExpiryDisabled,
		"blocksIncomingConnections": false,
		"created":                   tsTime(d.created),
		"expires":                   tsTime(d.expires),
		"lastSeen":                  tsTime(d.lastSeen),
		"machineKey":                "mkey:" + d.id,
		"nodeKey":                   "nodekey:" + d.id,
		"tags":                      d.tags,
	}
	if d.online {
		out["connectedToControl"] = true
		out["lastSeen"] = tsTime(time.Now())
	}
	if all {
		out["advertisedRoutes"] = d.advertisedRoutes
		out["enabledRoutes"] = d.enabledRoutes
		out["clientConnectivity"] = map[string]any{
			"endpoints": []string{"192.168.20.10:41641"}, "mappingVariesByDestIP": false,
		}
	}
	return out
}

func (s *Sim) keyPayload(k *key, withSecret bool) map[string]any {
	caps := map[string]any{
		"devices": map[string]any{
			"create": map[string]any{
				"reusable": k.reusable, "ephemeral": k.ephemeral,
				"preauthorized": k.preauthorized, "tags": k.tags,
			},
		},
	}
	out := map[string]any{
		"id":           k.id,
		"description":  k.description,
		"created":      tsTime(k.created),
		"expires":      tsTime(k.expires),
		"revoked":      tsTime(k.revoked),
		"invalid":      !k.revoked.IsZero() || time.Now().After(k.expires),
		"capabilities": caps,
	}
	if withSecret {
		out["key"] = k.secret
	}
	return out
}

func (s *Sim) reply(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	if payload == nil {
		w.Write([]byte("{}"))
		return
	}
	json.NewEncoder(w).Encode(payload)
}

func (s *Sim) fail(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	fmt.Fprintf(w, `{"message":%q}`, message)
}
