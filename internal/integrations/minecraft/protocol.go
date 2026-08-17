// Package minecraft implements the Minecraft companion integration.
//
// Unlike every other integration, the upstream connects to US: the companion
// plugin (Paper/Spigot, Velocity, Fabric, Forge) dials HomeHub's agent
// WebSocket, authenticates with a pairing code the first time (exchanged for
// a long-lived server token), and then streams console output, players, and
// performance metrics while accepting command execution requests.
//
// This file is the wire protocol, shared by the Go side and (by contract)
// the Java companion. Messages are single JSON objects; every frame has a
// "type". The companion speaks exactly this protocol regardless of loader.
package minecraft

import "encoding/json"

// Agent → HomeHub frame types.
const (
	// FramePair is the first frame on a virgin connection:
	// {"type":"pair","code":"123456","serverName":"lobby","loader":"paper",...}
	FramePair = "pair"
	// FrameHello authenticates a paired agent:
	// {"type":"hello","token":"...","serverName":...,"loader":...,"mcVersion":...}
	FrameHello = "hello"
	// FrameConsole carries one or more console lines in order.
	FrameConsole = "console"
	// FrameBackfill carries the pre-connection scrollback (latest.log) once,
	// right after hello/pair succeeds; lines precede everything live.
	FrameBackfill = "backfill"
	// FrameStatus carries the periodic performance snapshot.
	FrameStatus = "status"
	// FramePlayers carries the full player list (sent on join/leave/change).
	FramePlayers = "players"
	// FrameCmdResult reports a command execution request's outcome.
	FrameCmdResult = "cmd_result"
	// FramePong answers a ping.
	FramePong = "pong"
)

// HomeHub → agent frame types.
const (
	// FramePaired confirms pairing and delivers the permanent token.
	FramePaired = "paired"
	// FrameWelcome confirms hello.
	FrameWelcome = "welcome"
	// FrameCmd asks the server to execute a console command.
	FrameCmd = "cmd"
	// FramePing is the liveness probe.
	FramePing = "ping"
	// FrameErr reports a fatal protocol/auth error before closing.
	FrameErr = "error"
)

// Frame is the envelope every message uses.
type Frame struct {
	Type string `json:"type"`

	// pair / hello
	Code       string `json:"code,omitempty"`
	Token      string `json:"token,omitempty"`
	ServerName string `json:"serverName,omitempty"`
	Loader     string `json:"loader,omitempty"`    // paper|spigot|velocity|fabric|forge
	McVersion  string `json:"mcVersion,omitempty"` // e.g. 1.21.4
	Companion  string `json:"companion,omitempty"` // companion plugin version

	// console / backfill
	Lines []ConsoleLine `json:"lines,omitempty"`

	// status
	Status *StatusSnapshot `json:"status,omitempty"`

	// players
	Players []Player `json:"players,omitempty"`

	// cmd / cmd_result
	ID      int64  `json:"id,omitempty"`
	Command string `json:"command,omitempty"`
	OK      bool   `json:"ok,omitempty"`
	Message string `json:"message,omitempty"`
}

// ConsoleLine is one log line with the server-side timestamp (unix millis).
type ConsoleLine struct {
	TS    int64  `json:"ts"`
	Level string `json:"level,omitempty"` // INFO|WARN|ERROR|...
	Text  string `json:"text"`
}

// StatusSnapshot is the periodic performance report (every ~5s).
type StatusSnapshot struct {
	TPS           float64 `json:"tps,omitempty"`  // ticks per second (target 20)
	MSPT          float64 `json:"mspt,omitempty"` // mean ms per tick
	PlayersOnline int     `json:"playersOnline"`
	PlayersMax    int     `json:"playersMax"`
	HeapUsedMB    float64 `json:"heapUsedMb"`
	HeapMaxMB     float64 `json:"heapMaxMb"`
	// Velocity proxies report downstream servers instead of TPS.
	Downstream []DownstreamServer `json:"downstream,omitempty"`
}

// DownstreamServer is one backend behind a Velocity proxy.
type DownstreamServer struct {
	Name    string `json:"name"`
	Players int    `json:"players"`
}

// Player is one online player.
type Player struct {
	Name string `json:"name"`
	UUID string `json:"uuid,omitempty"`
}

// Decode unmarshals a raw agent frame.
func Decode(raw []byte) (*Frame, error) {
	var f Frame
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, err
	}
	return &f, nil
}
