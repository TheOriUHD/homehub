// Package ampsim is an in-memory CubeCoders AMP panel used to verify the
// AMP driver without a licensed installation. It mirrors AMP's real HTTP
// routing exactly — POST /API/{Module}/{Method}, the ADS proxy nesting at
// /API/ADSModule/Servers/{id}/API/..., per-scope SESSIONID auth issued by
// Core/Login, and the HTTP-200 {"Title":"Unauthorized Access"} envelope for
// dead sessions — so path or auth bugs in the driver fail here the way they
// would against the real panel.
package ampsim

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	// Fixed IDs so tests can address instances deterministically.
	ControllerID = "5f4c88a1-aaaa-4bbb-8ccc-00000000ad50"
	LobbyID      = "5f4c88a1-aaaa-4bbb-8ccc-00000010bb70"
	ValheimID    = "5f4c88a1-aaaa-4bbb-8ccc-0000007a1e00"
	RustID       = "5f4c88a1-aaaa-4bbb-8ccc-000000005357"
)

// Sim is a fake AMP controller with two managed instances.
type Sim struct {
	Username string
	Password string

	mu        sync.Mutex
	sessions  map[string]string       // sessionID -> scope it is valid for
	cursors   map[string]int          // sessionID -> console lines already delivered
	instances map[string]*simInstance // instanceID -> state
	order     []string                // stable instance order
}

type simInstance struct {
	id           string
	name         string
	friendlyName string
	description  string
	module       string
	moduleName   string
	containerUp  bool // the instance process itself; when false ADS cannot proxy
	appState     int
	suspended    bool
	port         int
	console      []consoleLine
	lastBeat     time.Time
	startedAt    time.Time
	pending      *pendingTransition
}

type pendingTransition struct {
	at    time.Time
	state int
}

type consoleLine struct {
	ts       time.Time
	source   string
	kind     string
	contents string
}

func New() *Sim {
	s := &Sim{
		Username:  "admin",
		Password:  "test123",
		sessions:  map[string]string{},
		cursors:   map[string]int{},
		instances: map[string]*simInstance{},
	}
	now := time.Now()
	s.add(&simInstance{
		id: ControllerID, name: "ADS01", friendlyName: "Controller",
		module: "ADS", moduleName: "Application Deployment Server",
		containerUp: true, appState: 20, port: 8080, startedAt: now.Add(-90 * time.Minute),
	})
	s.add(&simInstance{
		id: LobbyID, name: "Lobby01", friendlyName: "Lobby",
		description: "Velocity lobby server", module: "Minecraft", moduleName: "Minecraft",
		containerUp: true, appState: 20, port: 25565, startedAt: now.Add(-42 * time.Minute),
	})
	s.add(&simInstance{
		id: ValheimID, name: "Valheim01", friendlyName: "Valheim",
		description: "Weekend world", module: "GenericModule", moduleName: "Generic",
		containerUp: true, appState: 0, port: 2456,
	})
	s.add(&simInstance{
		id: RustID, name: "Rust01", friendlyName: "Rust",
		description: "Container stopped", module: "GenericModule", moduleName: "Generic",
		// Stale AppState on purpose: real AMP reports the last known app
		// state even when the instance container is down.
		containerUp: false, appState: 10, port: 28015,
	})
	for _, inst := range s.instances {
		if inst.appState == 20 {
			inst.console = append(inst.console, consoleLine{
				ts: inst.startedAt, source: "Server thread/INFO", kind: "Console",
				contents: "Done! Server started.",
			})
		}
	}
	return s
}

func (s *Sim) add(inst *simInstance) {
	s.instances[inst.id] = inst
	s.order = append(s.order, inst.id)
}

// ---- HTTP routing -----------------------------------------------------------

func (s *Sim) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	parts := strings.Split(path, "/")

	// Real routing: API/{Module}/{Method} on the controller, or
	// API/ADSModule/Servers/{instanceID}/API/{Module}/{Method} proxied.
	scope := ""
	var module, method string
	switch {
	case len(parts) == 6 && parts[0] == "API" && parts[1] == "ADSModule" && parts[2] == "Servers" && parts[4] == "API":
		// Proxied form with the module segment missing — invalid.
		http.NotFound(w, r)
		return
	case len(parts) == 7 && parts[0] == "API" && parts[1] == "ADSModule" && parts[2] == "Servers" && parts[4] == "API":
		scope, module, method = parts[3], parts[5], parts[6]
		inst, ok := s.instances[scope]
		if !ok || scope == ControllerID || !inst.containerUp {
			s.reply(w, map[string]any{
				"Title":   "Instance Unavailable",
				"Message": "The requested instance is not available at this time.",
			})
			return
		}
	case len(parts) == 3 && parts[0] == "API":
		module, method = parts[1], parts[2]
	default:
		http.NotFound(w, r)
		return
	}

	var params map[string]any
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.settleTransitions()

	if module == "Core" && method == "Login" {
		s.handleLogin(w, scope, params)
		return
	}

	sessionID, _ := params["SESSIONID"].(string)
	if s.sessions[sessionID] != sessionKey(scope) {
		s.reply(w, map[string]any{"Title": "Unauthorized Access"})
		return
	}

	switch module + "/" + method {
	case "ADSModule/GetInstances":
		if scope != "" {
			s.reply(w, map[string]any{"Title": "Unauthorized Access"})
			return
		}
		s.reply(w, s.targetsPayload())
	case "ADSModule/StartInstance", "ADSModule/StopInstance", "ADSModule/RestartInstance":
		if scope != "" {
			s.reply(w, map[string]any{"Title": "Unauthorized Access"})
			return
		}
		s.handleInstanceLifecycle(w, method, params)
	case "Core/GetStatus":
		inst := s.instances[scopeOr(scope, ControllerID)]
		s.reply(w, s.statusPayload(inst))
	case "Core/GetUpdates":
		inst := s.instances[scopeOr(scope, ControllerID)]
		s.emitHeartbeat(inst)
		start := s.cursors[sessionID]
		if start > len(inst.console) {
			start = 0
		}
		entries := make([]map[string]any, 0, len(inst.console)-start)
		for _, line := range inst.console[start:] {
			entries = append(entries, map[string]any{
				"Timestamp": line.ts.Format(time.RFC3339),
				"Source":    line.source,
				"Type":      line.kind,
				"Contents":  line.contents,
			})
		}
		s.cursors[sessionID] = len(inst.console)
		s.reply(w, map[string]any{
			"Status":         s.statusPayload(inst),
			"ConsoleEntries": entries,
			"Messages":       []any{},
			"Tasks":          []any{},
			"Ports":          []any{},
		})
	case "Core/SendConsoleMessage":
		inst := s.instances[scopeOr(scope, ControllerID)]
		msg, _ := params["message"].(string)
		s.runCommand(inst, msg)
		s.reply(w, nil)
	case "Core/Start":
		s.appTransition(s.instances[scopeOr(scope, ControllerID)], 10, 20, "Starting the application.")
		s.reply(w, map[string]any{"Status": true})
	case "Core/Stop":
		s.appTransition(s.instances[scopeOr(scope, ControllerID)], 40, 0, "Stopping the application.")
		s.reply(w, nil)
	case "Core/Restart":
		s.appTransition(s.instances[scopeOr(scope, ControllerID)], 30, 20, "Restarting the application.")
		s.reply(w, map[string]any{"Status": true})
	case "Core/Kill":
		inst := s.instances[scopeOr(scope, ControllerID)]
		inst.appState = 0
		inst.pending = nil
		s.appendConsole(inst, "Console", "Process killed.")
		s.reply(w, nil)
	case "Core/UpdateApplication":
		s.reply(w, map[string]any{"Status": true})
	case "Core/Logout":
		delete(s.sessions, sessionID)
		delete(s.cursors, sessionID)
		s.reply(w, nil)
	default:
		http.NotFound(w, r)
	}
}

func (s *Sim) handleLogin(w http.ResponseWriter, scope string, params map[string]any) {
	user, _ := params["username"].(string)
	pass, _ := params["password"].(string)
	if user != s.Username || pass != s.Password {
		s.reply(w, map[string]any{
			"success": false, "result": 1,
			"resultReason": "Invalid username or password", "sessionID": "",
		})
		return
	}
	id := uuid.NewString()
	s.sessions[id] = sessionKey(scope)
	// A fresh session's first GetUpdates delivers the recent console tail,
	// like the real panel's UI shows on load.
	s.cursors[id] = max(0, len(s.instances[scopeOr(scope, ControllerID)].console)-50)
	s.reply(w, map[string]any{
		"success": true, "result": 10, "resultReason": "",
		"sessionID": id, "rememberMeToken": "", "permissions": []string{},
		"userInfo": map[string]any{"ID": uuid.NewString(), "Username": user},
	})
}

func (s *Sim) handleInstanceLifecycle(w http.ResponseWriter, method string, params map[string]any) {
	name, _ := params["InstanceName"].(string)
	var target *simInstance
	for _, inst := range s.instances {
		if inst.name == name {
			target = inst
		}
	}
	if target == nil || target.id == ControllerID {
		s.reply(w, map[string]any{"Status": false, "Reason": "Unknown instance '" + name + "'"})
		return
	}
	switch method {
	case "StartInstance":
		s.appTransition(target, 10, 20, "Instance started via ADS.")
	case "StopInstance":
		s.appTransition(target, 40, 0, "Instance stopped via ADS.")
	case "RestartInstance":
		s.appTransition(target, 30, 20, "Instance restarted via ADS.")
	}
	s.reply(w, map[string]any{"Status": true})
}

// ---- state helpers ----------------------------------------------------------

func sessionKey(scope string) string {
	if scope == "" {
		return "controller"
	}
	return scope
}

func scopeOr(scope, fallback string) string {
	if scope == "" {
		return fallback
	}
	return scope
}

// appTransition moves an instance through an intermediate state that
// settles to the final one shortly after — mirrors real AMP behavior where
// Starting/Stopping are observable.
func (s *Sim) appTransition(inst *simInstance, intermediate, final int, note string) {
	inst.appState = intermediate
	inst.pending = &pendingTransition{at: time.Now().Add(1500 * time.Millisecond), state: final}
	if final == 20 {
		inst.startedAt = time.Now()
	}
	s.appendConsole(inst, "Console", note)
}

func (s *Sim) settleTransitions() {
	now := time.Now()
	for _, inst := range s.instances {
		if inst.pending != nil && now.After(inst.pending.at) {
			inst.appState = inst.pending.state
			if inst.appState == 20 {
				s.appendConsole(inst, "Console", "Done! Server started.")
			} else if inst.appState == 0 {
				s.appendConsole(inst, "Console", "Server stopped.")
			}
			inst.pending = nil
		}
	}
}

func (s *Sim) appendConsole(inst *simInstance, kind, contents string) {
	inst.console = append(inst.console, consoleLine{
		ts: time.Now(), source: "Server thread/INFO", kind: kind, contents: contents,
	})
}

// emitHeartbeat drips a background line into running consoles so live
// streaming is observable.
func (s *Sim) emitHeartbeat(inst *simInstance) {
	if inst.appState != 20 || time.Since(inst.lastBeat) < 5*time.Second {
		return
	}
	inst.lastBeat = time.Now()
	s.appendConsole(inst, "Console", fmt.Sprintf("Saving chunks for level 'world' (uptime %s)",
		time.Since(inst.startedAt).Truncate(time.Second)))
}

func (s *Sim) runCommand(inst *simInstance, command string) {
	command = strings.TrimSpace(command)
	s.appendConsole(inst, "Chat", "> "+command)
	switch {
	case command == "":
	case strings.HasPrefix(command, "say "):
		s.appendConsole(inst, "Chat", "[Server] "+strings.TrimPrefix(command, "say "))
	case command == "list":
		s.appendConsole(inst, "Console", "There are 2 of a max of 20 players online: alice, bob")
	case command == "stop":
		s.appTransition(inst, 40, 0, "Stopping the server.")
	default:
		s.appendConsole(inst, "Error", "Unknown command '"+command+"'")
	}
}

// ---- payloads ---------------------------------------------------------------

func (s *Sim) metricsPayload(inst *simInstance) map[string]any {
	if inst.appState != 20 {
		return map[string]any{}
	}
	// Deterministic wiggle keyed to the clock so graphs move.
	wave := math.Sin(float64(time.Now().Unix()%60) / 60 * 2 * math.Pi)
	cpu := 12.0 + 8*wave
	if inst.module == "ADS" {
		cpu = 2.0 + wave
	}
	memMax := 4096.0
	memUsed := 1500.0 + 400*wave
	players := 2
	out := map[string]any{
		"CPU Usage": map[string]any{
			"RawValue": math.Round(cpu*10) / 10, "MaxValue": 100,
			"Percent": math.Round(cpu*10) / 10, "Units": "%",
		},
		"Memory Usage": map[string]any{
			"RawValue": math.Round(memUsed), "MaxValue": memMax,
			"Percent": math.Round(memUsed / memMax * 100), "Units": "MB",
		},
	}
	if inst.module == "Minecraft" {
		out["Active Users"] = map[string]any{
			"RawValue": players, "MaxValue": 20,
			"Percent": players * 100 / 20, "Units": "",
		}
	}
	return out
}

func (s *Sim) instancePayload(inst *simInstance) map[string]any {
	return map[string]any{
		"InstanceID":           inst.id,
		"TargetID":             ControllerID,
		"InstanceName":         inst.name,
		"FriendlyName":         inst.friendlyName,
		"Description":          inst.description,
		"Module":               inst.module,
		"ModuleDisplayName":    inst.moduleName,
		"AMPVersion":           map[string]any{"Major": 2, "Minor": 6, "Build": 2},
		"IsHTTPS":              false,
		"IP":                   "127.0.0.1",
		"Port":                 inst.port,
		"Daemon":               false,
		"Running":              inst.containerUp,
		"AppState":             inst.appState,
		"Tags":                 []string{},
		"DiskUsageMB":          2048,
		"Suspended":            inst.suspended,
		"IsContainerInstance":  false,
		"Metrics":              s.metricsPayload(inst),
		"ApplicationEndpoints": []any{},
		"DisplayImageSource":   "",
	}
}

func (s *Sim) targetsPayload() []map[string]any {
	instances := make([]map[string]any, 0, len(s.order))
	for _, id := range s.order {
		instances = append(instances, s.instancePayload(s.instances[id]))
	}
	return []map[string]any{{
		"Id":           1,
		"InstanceId":   ControllerID,
		"FriendlyName": "Local",
		"Description":  "",
		"Disabled":     false,
		"isRemote":     false,
		"Platform": map[string]any{
			"CPUInfo": map[string]any{
				"Sockets": 1, "Cores": 8, "Threads": 16,
				"Vendor": "AMD", "ModelName": "Ryzen 7 5800X",
				"TotalCores": 8, "TotalThreads": 16,
			},
			"InstalledRAMMB": 32768,
			"PlatformName":   "Linux",
			"SystemType":     2,
			"Virtualization": 0,
		},
		"Datastores":          []any{map[string]any{"Id": 1, "FriendlyName": "Default"}},
		"CreatesInContainers": false,
		"State":               20,
		"StateReason":         "",
		"CanCreate":           true,
		"LastUpdated":         time.Now().Format(time.RFC3339),
		"AvailableInstances":  instances,
		"AvailableIPs":        []string{"127.0.0.1"},
		"URL":                 "http://127.0.0.1:8080/",
		"Tags":                []any{},
		"TagsNames":           []any{},
	}}
}

func (s *Sim) statusPayload(inst *simInstance) map[string]any {
	uptime := "0:00:00:00"
	if inst.appState == 20 {
		d := time.Since(inst.startedAt)
		uptime = fmt.Sprintf("%d:%02d:%02d:%02d",
			int(d.Hours())/24, int(d.Hours())%24, int(d.Minutes())%60, int(d.Seconds())%60)
	}
	return map[string]any{
		"State":   inst.appState,
		"Uptime":  uptime,
		"Metrics": s.metricsPayload(inst),
	}
}

// ExpireSessions invalidates every issued session — tests use it to prove
// the client re-authenticates transparently.
func (s *Sim) ExpireSessions() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions = map[string]string{}
	s.cursors = map[string]int{}
}

func (s *Sim) reply(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	if payload == nil {
		w.Write([]byte("null"))
		return
	}
	json.NewEncoder(w).Encode(payload)
}
