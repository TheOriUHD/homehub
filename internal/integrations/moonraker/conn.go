package moonraker

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"homehub/internal/integration"
	"homehub/internal/metrics"
)

// ---- object discovery -------------------------------------------------------

// objectList returns every Klipper object the printer currently exposes.
func (c *client) objectList(ctx context.Context) ([]string, error) {
	var out struct {
		Objects []string `json:"objects"`
	}
	if err := c.get(ctx, "/printer/objects/list", &out); err != nil {
		return nil, err
	}
	return out.Objects, nil
}

// queryStatus fetches the full state of the named objects in one round-trip.
// Object names go on the query string bare (no "="), which asks Moonraker for
// every field of each object; names with spaces are percent-encoded.
func (c *client) queryStatus(ctx context.Context, names []string) (map[string]objFields, error) {
	if len(names) == 0 {
		return map[string]objFields{}, nil
	}
	parts := make([]string, 0, len(names))
	for _, n := range names {
		parts = append(parts, url.QueryEscape(n))
	}
	var out struct {
		Status    map[string]objFields `json:"status"`
		EventTime float64              `json:"eventtime"`
	}
	if err := c.get(ctx, "/printer/objects/query?"+strings.Join(parts, "&"), &out); err != nil {
		return nil, err
	}
	if out.Status == nil {
		out.Status = map[string]objFields{}
	}
	return out.Status, nil
}

// objFields is one Klipper object's fields, left raw so each classifier reads
// only what it needs (printers add fields freely between Klipper versions).
type objFields map[string]json.RawMessage

func (o objFields) float(key string) (float64, bool) {
	raw, ok := o[key]
	if !ok {
		return 0, false
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err != nil {
		return 0, false
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return f, true
}

func (o objFields) str(key string) string {
	raw, ok := o[key]
	if !ok {
		return ""
	}
	var s string
	_ = json.Unmarshal(raw, &s)
	return s
}

func (o objFields) floats(key string) []float64 {
	raw, ok := o[key]
	if !ok {
		return nil
	}
	var out []float64
	_ = json.Unmarshal(raw, &out)
	return out
}

// ---- snapshot shape (mirrored by the OpenAPI MoonrakerSnapshot schema) ------

type Snapshot struct {
	Klippy     KlippyState `json:"klippy"`
	Printer    PrinterMeta `json:"printer"`
	Print      *PrintJob   `json:"print,omitempty"`
	Toolhead   *Toolhead   `json:"toolhead,omitempty"`
	Heaters    []Heater    `json:"heaters"`
	Sensors    []Sensor    `json:"sensors"`
	TempFans   []TempFan   `json:"tempFans"`
	Fans       []Fan       `json:"fans"`
	Leds       []Led       `json:"leds"`
	OutputPins []OutputPin `json:"outputPins"`
	Macros     []Macro     `json:"macros"`
	Cameras    []Camera    `json:"cameras"`
	Extra      []string    `json:"extra"`
	UpdatedAt  time.Time   `json:"updatedAt"`
	Live       bool        `json:"live"`
}

// KlippyState is the firmware connection state — the top-level health.
type KlippyState struct {
	State   string `json:"state"`   // ready | startup | shutdown | error | disconnected
	Message string `json:"message"` // Klipper's own status/error text
}

type PrinterMeta struct {
	Hostname       string `json:"hostname"`
	KlipperVersion string `json:"klipperVersion"`
	MoonrakerVer   string `json:"moonrakerVersion"`
	CPUInfo        string `json:"cpuInfo"`
}

// PrintJob merges print_stats + virtual_sdcard + display_status.
type PrintJob struct {
	State         string  `json:"state"` // standby|printing|paused|complete|cancelled|error
	Filename      string  `json:"filename"`
	Progress      float64 `json:"progress"` // 0..1
	Message       string  `json:"message"`
	PrintDuration float64 `json:"printDuration"` // seconds of actual printing
	TotalDuration float64 `json:"totalDuration"` // seconds wall-clock
	FilamentUsed  float64 `json:"filamentUsed"`  // mm
	CurrentLayer  int     `json:"currentLayer"`
	TotalLayer    int     `json:"totalLayer"`
}

type Toolhead struct {
	Position      []float64 `json:"position"`           // [x,y,z,e]
	HomedAxes     string    `json:"homedAxes"`          // e.g. "xyz"
	SpeedFactor   float64   `json:"speedFactor"`        // 1.0 = 100%
	ExtrudeFactor float64   `json:"extrudeFactor"`      // 1.0 = 100%
	FanSpeed      *float64  `json:"fanSpeed,omitempty"` // part fan 0..1, if present
	MaxVelocity   float64   `json:"maxVelocity"`
	Speed         float64   `json:"speed"` // current requested speed (mm/s)
}

// Heater is anything with a controllable target: extruders, the bed, and
// heater_generic devices (chamber, etc.).
type Heater struct {
	Object string  `json:"object"` // full Klipper object name (for control gcode)
	Name   string  `json:"name"`   // display name
	Kind   string  `json:"kind"`   // extruder | bed | generic
	Temp   float64 `json:"temp"`
	Target float64 `json:"target"`
	Power  float64 `json:"power"` // 0..1 duty
}

// Sensor is a read-only temperature_sensor.
type Sensor struct {
	Object  string   `json:"object"`
	Name    string   `json:"name"`
	Temp    float64  `json:"temp"`
	MinTemp *float64 `json:"minTemp,omitempty"`
	MaxTemp *float64 `json:"maxTemp,omitempty"`
}

// TempFan is a temperature_fan — a fan driven by a temperature target.
type TempFan struct {
	Object string  `json:"object"`
	Name   string  `json:"name"`
	Temp   float64 `json:"temp"`
	Target float64 `json:"target"`
	Speed  float64 `json:"speed"` // 0..1
}

// Fan is any non-thermostatic fan.
type Fan struct {
	Object       string   `json:"object"`
	Name         string   `json:"name"`
	Kind         string   `json:"kind"`  // part | generic | heater | controller
	Speed        float64  `json:"speed"` // 0..1
	RPM          *float64 `json:"rpm,omitempty"`
	Controllable bool     `json:"controllable"` // part & generic fans accept a speed
}

// Led is a controllable light: led / neopixel / dotstar. Color is the first
// pixel's RGBW (0..1), which is what a single-color control edits.
type Led struct {
	Object string    `json:"object"`
	Name   string    `json:"name"`
	Kind   string    `json:"kind"`
	Color  []float64 `json:"color"` // [r,g,b,w] 0..1
	Pixels int       `json:"pixels"`
}

// OutputPin is a generic output — either digital (0/1) or PWM (0..1).
type OutputPin struct {
	Object string  `json:"object"`
	Name   string  `json:"name"`
	Value  float64 `json:"value"`
	PWM    bool    `json:"pwm"`
}

// Macro is a gcode_macro the operator can run.
type Macro struct {
	Object      string `json:"object"` // full object name "gcode_macro FOO"
	Name        string `json:"name"`   // "FOO" — what you send to run it
	Description string `json:"description,omitempty"`
}

// ---- connection -------------------------------------------------------------

type Conn struct {
	inst   integration.Instance
	client *client
	deps   integration.Deps

	mu           sync.Mutex
	objects      []string             // discovered object names
	subNames     []string             // the subset we query/subscribe for state
	macros       []Macro              // discovered macros (with descriptions)
	discAt       time.Time            // last discovery
	state        map[string]objFields // live merged state (from WS, seeded by REST)
	stateAt      time.Time            // when state was last refreshed
	live         bool                 // true while the WS subscription is up
	meta         PrinterMeta          // last REST-fetched printer meta (for live snapshots)
	lastPublish  time.Time            // coalesces high-rate WS snapshot pushes
	webcamsCache []webcamWire         // cached /server/webcams/list
	webcamsAt    time.Time            // webcam cache timestamp

	cancel context.CancelFunc // stops the WS loop
}

func newConn(inst integration.Instance, c *client, deps integration.Deps) *Conn {
	ctx, cancel := context.WithCancel(context.Background())
	conn := &Conn{
		inst:   inst,
		client: c,
		deps:   deps,
		state:  map[string]objFields{},
		cancel: cancel,
	}
	go conn.runWS(ctx)
	return conn
}

func (c *Conn) Close() {
	if c.cancel != nil {
		c.cancel()
	}
}

// BaseURL is the Moonraker origin, for the "Open Moonraker" deep link.
func (c *Conn) BaseURL() string { return c.client.base }

// discover refreshes the object list and macro descriptions. Cheap enough to
// re-run periodically so a printer that gains a macro or sensor shows it
// without a HomeHub restart.
func (c *Conn) discover(ctx context.Context) error {
	list, err := c.client.objectList(ctx)
	if err != nil {
		return err
	}
	sub := subscribeSet(list)
	macros := macrosFrom(list)
	// Macro descriptions live in the config, not object state — fetch once per
	// discovery (best-effort; a missing description just omits the tooltip).
	descs := c.macroDescriptions(ctx)
	for i := range macros {
		if d := descs[strings.ToLower(macros[i].Name)]; d != "" {
			macros[i].Description = d
		}
	}
	c.mu.Lock()
	c.objects, c.subNames, c.macros, c.discAt = list, sub, macros, time.Now()
	c.mu.Unlock()
	return nil
}

// macroDescriptions pulls each gcode_macro's `description` from the live
// config (configfile.settings). Best-effort.
func (c *Conn) macroDescriptions(ctx context.Context) map[string]string {
	status, err := c.client.queryStatus(ctx, []string{"configfile"})
	out := map[string]string{}
	if err != nil {
		return out
	}
	cfg, ok := status["configfile"]
	if !ok {
		return out
	}
	raw, ok := cfg["settings"]
	if !ok {
		return out
	}
	var settings map[string]struct {
		Description string `json:"description"`
	}
	if err := json.Unmarshal(raw, &settings); err != nil {
		return out
	}
	for key, s := range settings {
		if name, ok := strings.CutPrefix(key, "gcode_macro "); ok && s.Description != "" {
			out[strings.ToLower(strings.TrimSpace(name))] = s.Description
		}
	}
	return out
}

// Snapshot returns the full dynamic printer state. It prefers the live WS
// state (instant, shared across all HomeHub clients) and falls back to a REST
// query when the socket is down or has no data yet.
func (c *Conn) Snapshot(ctx context.Context) (*Snapshot, error) {
	c.mu.Lock()
	needDiscover := c.discAt.IsZero() || time.Since(c.discAt) > 5*time.Minute
	c.mu.Unlock()
	if needDiscover {
		if err := c.discover(ctx); err != nil {
			return nil, err
		}
	}

	c.mu.Lock()
	live := c.live
	stateFresh := live && !c.stateAt.IsZero() && time.Since(c.stateAt) < 10*time.Second
	names := append([]string(nil), c.subNames...)
	macros := append([]Macro(nil), c.macros...)
	objects := append([]string(nil), c.objects...)
	var state map[string]objFields
	if stateFresh {
		state = cloneState(c.state)
	}
	c.mu.Unlock()

	// serverInfo also proves reachability — if it fails the printer is down and
	// the dashboard should show an error, not an empty board.
	srv, srvErr := c.client.serverInfo(ctx)
	info, _ := c.client.printerInfo(ctx)

	if state == nil {
		// No fresh live data — do a one-shot REST query and seed the cache.
		fetched, err := c.client.queryStatus(ctx, names)
		if err != nil {
			if srvErr != nil {
				return nil, err // genuinely unreachable
			}
			// Reachable but Klippy can't be queried (shutdown / error / startup):
			// return an empty board so the Klippy banner and state still render,
			// and don't poison the live cache with the empty result.
			state = map[string]objFields{}
		} else {
			state = fetched
			c.mu.Lock()
			c.state, c.stateAt = cloneState(fetched), time.Now()
			c.mu.Unlock()
		}
	}

	snap := buildSnapshot(objects, state, macros)
	snap.Live = live
	if srv != nil {
		snap.Klippy.State = srv.KlippyState
		snap.Printer.MoonrakerVer = srv.MoonrakerVer
	}
	if info != nil {
		snap.Printer.Hostname = info.Hostname
		snap.Printer.KlipperVersion = info.SoftwareVersion
		snap.Printer.CPUInfo = info.CPUInfo
		if snap.Klippy.State == "" {
			snap.Klippy.State = info.State
		}
		if info.StateMessage != "" {
			snap.Klippy.Message = strings.TrimSpace(info.StateMessage)
		}
	}
	// webhooks carries the authoritative klippy state+message when present.
	if wh, ok := state["webhooks"]; ok {
		if s := wh.str("state"); s != "" {
			snap.Klippy.State = s
		}
		if m := wh.str("state_message"); m != "" {
			snap.Klippy.Message = strings.TrimSpace(m)
		}
	}
	// Cache the meta so live WS snapshots (which don't hit REST) carry the
	// header identity too.
	c.mu.Lock()
	c.meta = snap.Printer
	c.mu.Unlock()
	if cams := c.Cameras(ctx); cams != nil {
		snap.Cameras = cams
	}
	return snap, nil
}

// ---- actions ----------------------------------------------------------------

// RunGcode sends a raw G-code script (a macro name, "G28", "M106 S128",
// "SET_LED LED=x RED=1", …). This is the universal control surface — the
// dashboard composes fan/LED/home/macro commands and routes them here.
func (c *Conn) RunGcode(ctx context.Context, script string) error {
	script = strings.TrimSpace(script)
	if script == "" {
		return fmt.Errorf("empty g-code")
	}
	return c.client.post(ctx, "/printer/gcode/script?script="+url.QueryEscape(script))
}

// SetHeaterTarget sets a heater's target temperature via SET_HEATER_TEMPERATURE,
// which works uniformly for extruders, the bed and heater_generic devices.
// The gcode addresses a heater by its Klipper config name — the object name
// without its type prefix ("heater_generic chamber" → "chamber"), while
// "extruder" and "heater_bed" (no prefix) pass through unchanged.
func (c *Conn) SetHeaterTarget(ctx context.Context, object string, target float64) error {
	if target < 0 {
		target = 0
	}
	name := object
	if i := strings.IndexByte(object, ' '); i >= 0 {
		name = object[i+1:]
	}
	return c.RunGcode(ctx, fmt.Sprintf("SET_HEATER_TEMPERATURE HEATER=%s TARGET=%.0f", name, target))
}

// PrintAction runs pause/resume/cancel on the active print.
func (c *Conn) PrintAction(ctx context.Context, action string) error {
	switch action {
	case "pause", "resume", "cancel":
		return c.client.post(ctx, "/printer/print/"+action)
	}
	return fmt.Errorf("unknown print action %q", action)
}

// EmergencyStop halts the printer immediately (Klipper enters shutdown).
func (c *Conn) EmergencyStop(ctx context.Context) error {
	return c.client.post(ctx, "/printer/emergency_stop")
}

// FirmwareRestart recovers Klipper after a shutdown/error.
func (c *Conn) FirmwareRestart(ctx context.Context) error {
	return c.client.post(ctx, "/printer/firmware_restart")
}

// ---- platform hooks ---------------------------------------------------------

func (c *Conn) Health(ctx context.Context) integration.Health {
	srv, err := c.client.serverInfo(ctx)
	if err != nil {
		return integration.Health{Status: integration.StatusUnreachable, Message: friendlyNetErr(err).Error()}
	}
	switch srv.KlippyState {
	case "ready":
		return integration.Health{Status: integration.StatusConnected}
	case "startup":
		return integration.Health{Status: integration.StatusConnecting, Message: "Klipper is starting up"}
	case "shutdown", "error":
		msg := "Klipper is in " + srv.KlippyState
		if info, ierr := c.client.printerInfo(ctx); ierr == nil && info.StateMessage != "" {
			msg = strings.TrimSpace(firstLine(info.StateMessage))
		}
		return integration.Health{Status: integration.StatusDegraded, Message: msg}
	default:
		// Reachable but Klippy not connected — Moonraker is up, printer host may
		// be booting. Degraded rather than down: the API answers.
		if !srv.KlippyConnected {
			return integration.Health{Status: integration.StatusDegraded, Message: "Klipper host is not connected to Moonraker"}
		}
		return integration.Health{Status: integration.StatusConnected}
	}
}

func (c *Conn) Resources(ctx context.Context) ([]integration.Resource, error) {
	// A printer is controlled as a unit; the instance itself is the resource.
	return nil, nil
}

func (c *Conn) Collect(ctx context.Context) ([]metrics.Sample, error) {
	snap, err := c.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	base := TypeID + "/" + c.inst.ID.String()
	var samples []metrics.Sample
	for _, h := range snap.Heaters {
		res := base + "/heater/" + h.Object
		samples = append(samples,
			metrics.Sample{Resource: res, Metric: "temperature", Unit: "celsius", TS: now, Value: h.Temp},
			metrics.Sample{Resource: res, Metric: "target", Unit: "celsius", TS: now, Value: h.Target},
		)
	}
	for _, s := range snap.Sensors {
		samples = append(samples, metrics.Sample{Resource: base + "/sensor/" + s.Object, Metric: "temperature", Unit: "celsius", TS: now, Value: s.Temp})
	}
	for _, f := range snap.TempFans {
		samples = append(samples, metrics.Sample{Resource: base + "/tempfan/" + f.Object, Metric: "temperature", Unit: "celsius", TS: now, Value: f.Temp})
	}
	if snap.Print != nil && (snap.Print.State == "printing" || snap.Print.State == "paused") {
		samples = append(samples, metrics.Sample{Resource: base, Metric: "print_progress", Unit: "percent", TS: now, Value: snap.Print.Progress * 100})
	}
	return samples, nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func cloneState(in map[string]objFields) map[string]objFields {
	out := make(map[string]objFields, len(in))
	for k, v := range in {
		cp := make(objFields, len(v))
		for fk, fv := range v {
			cp[fk] = fv
		}
		out[k] = cp
	}
	return out
}

// ---- classification ---------------------------------------------------------

// heaterPrefixes / etc. — the Klipper object prefixes we know how to render.

func splitObject(name string) (kind, sub string) {
	if i := strings.IndexByte(name, ' '); i >= 0 {
		return name[:i], name[i+1:]
	}
	return name, ""
}

// subscribeSet is the object subset whose state we track (everything with a
// live value; macros are excluded — they carry no useful state).
func subscribeSet(objects []string) []string {
	var out []string
	for _, o := range objects {
		kind, _ := splitObject(o)
		switch {
		case kind == "extruder" || strings.HasPrefix(kind, "extruder"):
			out = append(out, o)
		case kind == "heater_bed" || kind == "heater_generic":
			out = append(out, o)
		case kind == "temperature_sensor" || kind == "temperature_fan":
			out = append(out, o)
		case kind == "fan" || kind == "fan_generic" || kind == "heater_fan" || kind == "controller_fan":
			out = append(out, o)
		case kind == "led" || kind == "neopixel" || kind == "dotstar" || kind == "pca9533" || kind == "pca9632":
			out = append(out, o)
		case kind == "output_pin":
			out = append(out, o)
		case o == "print_stats" || o == "display_status" || o == "virtual_sdcard" ||
			o == "toolhead" || o == "gcode_move" || o == "webhooks" || o == "idle_timeout":
			out = append(out, o)
		}
	}
	return out
}

func macrosFrom(objects []string) []Macro {
	var out []Macro
	for _, o := range objects {
		if name, ok := strings.CutPrefix(o, "gcode_macro "); ok {
			out = append(out, Macro{Object: o, Name: name})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// buildSnapshot classifies the object state into the dashboard shape.
func buildSnapshot(objects []string, state map[string]objFields, macros []Macro) *Snapshot {
	snap := &Snapshot{
		Heaters: []Heater{}, Sensors: []Sensor{}, TempFans: []TempFan{},
		Fans: []Fan{}, Leds: []Led{}, OutputPins: []OutputPin{}, Cameras: []Camera{}, Extra: []string{},
		Macros: macros, UpdatedAt: time.Now(),
	}
	if snap.Macros == nil {
		snap.Macros = []Macro{}
	}
	for _, o := range objects {
		kind, sub := splitObject(o)
		f := state[o]
		switch {
		case kind == "extruder" || (strings.HasPrefix(kind, "extruder") && isIndexed(kind, "extruder")):
			snap.Heaters = append(snap.Heaters, heaterFrom(o, extruderLabel(kind), "extruder", f))
		case kind == "heater_bed":
			snap.Heaters = append(snap.Heaters, heaterFrom(o, "Bed", "bed", f))
		case kind == "heater_generic":
			snap.Heaters = append(snap.Heaters, heaterFrom(o, prettify(sub), "generic", f))
		case kind == "temperature_sensor":
			s := Sensor{Object: o, Name: prettify(sub)}
			s.Temp, _ = f.float("temperature")
			if mn, ok := f.float("measured_min_temp"); ok {
				s.MinTemp = &mn
			}
			if mx, ok := f.float("measured_max_temp"); ok {
				s.MaxTemp = &mx
			}
			snap.Sensors = append(snap.Sensors, s)
		case kind == "temperature_fan":
			tf := TempFan{Object: o, Name: prettify(sub)}
			tf.Temp, _ = f.float("temperature")
			tf.Target, _ = f.float("target")
			tf.Speed, _ = f.float("speed")
			snap.TempFans = append(snap.TempFans, tf)
		case kind == "fan":
			snap.Fans = append(snap.Fans, fanFrom(o, "Part cooling", "part", true, f))
		case kind == "fan_generic":
			snap.Fans = append(snap.Fans, fanFrom(o, prettify(sub), "generic", true, f))
		case kind == "heater_fan":
			snap.Fans = append(snap.Fans, fanFrom(o, prettify(sub), "heater", false, f))
		case kind == "controller_fan":
			snap.Fans = append(snap.Fans, fanFrom(o, prettify(sub), "controller", false, f))
		case kind == "led" || kind == "neopixel" || kind == "dotstar" || kind == "pca9533" || kind == "pca9632":
			snap.Leds = append(snap.Leds, ledFrom(o, prettify(sub), kind, f))
		case kind == "output_pin":
			v, _ := f.float("value")
			snap.OutputPins = append(snap.OutputPins, OutputPin{Object: o, Name: prettify(sub), Value: v, PWM: v > 0 && v < 1})
		case o == "print_stats" || o == "display_status" || o == "virtual_sdcard" ||
			o == "toolhead" || o == "gcode_move" || o == "webhooks" || o == "idle_timeout" ||
			kind == "gcode_macro" || kind == "configfile" || kind == "mcu":
			// consumed elsewhere (print/toolhead) or intentionally ignored
		default:
			snap.Extra = append(snap.Extra, o)
		}
	}

	snap.Print = printJobFrom(state)
	snap.Toolhead = toolheadFrom(state)
	return snap
}

func isIndexed(kind, prefix string) bool {
	rest := strings.TrimPrefix(kind, prefix)
	if rest == "" {
		return true
	}
	_, err := strconv.Atoi(rest)
	return err == nil
}

func extruderLabel(kind string) string {
	if kind == "extruder" {
		return "Extruder"
	}
	if n := strings.TrimPrefix(kind, "extruder"); n != "" {
		return "Extruder " + n
	}
	return "Extruder"
}

func heaterFrom(object, name, kind string, f objFields) Heater {
	h := Heater{Object: object, Name: name, Kind: kind}
	h.Temp, _ = f.float("temperature")
	h.Target, _ = f.float("target")
	h.Power, _ = f.float("power")
	return h
}

func fanFrom(object, name, kind string, controllable bool, f objFields) Fan {
	fan := Fan{Object: object, Name: name, Kind: kind, Controllable: controllable}
	fan.Speed, _ = f.float("speed")
	if rpm, ok := f.float("rpm"); ok {
		fan.RPM = &rpm
	}
	return fan
}

func ledFrom(object, name, kind string, f objFields) Led {
	led := Led{Object: object, Name: name, Kind: kind, Color: []float64{0, 0, 0, 0}}
	// color_data is [[r,g,b,w], …] per pixel; edit the first pixel.
	if raw, ok := f["color_data"]; ok {
		var pixels [][]float64
		if json.Unmarshal(raw, &pixels) == nil {
			led.Pixels = len(pixels)
			if len(pixels) > 0 {
				led.Color = padColor(pixels[0])
			}
		}
	}
	return led
}

func padColor(c []float64) []float64 {
	out := []float64{0, 0, 0, 0}
	for i := 0; i < len(c) && i < 4; i++ {
		out[i] = c[i]
	}
	return out
}

func printJobFrom(state map[string]objFields) *PrintJob {
	ps, hasPS := state["print_stats"]
	vsd := state["virtual_sdcard"]
	disp := state["display_status"]
	if !hasPS {
		return nil
	}
	job := &PrintJob{
		State:    ps.str("state"),
		Filename: ps.str("filename"),
		Message:  ps.str("message"),
	}
	job.PrintDuration, _ = ps.float("print_duration")
	job.TotalDuration, _ = ps.float("total_duration")
	job.FilamentUsed, _ = ps.float("filament_used")
	// Layer info lives in print_stats.info as {current_layer,total_layer}.
	if raw, ok := ps["info"]; ok {
		var info struct {
			CurrentLayer *int `json:"current_layer"`
			TotalLayer   *int `json:"total_layer"`
		}
		if json.Unmarshal(raw, &info) == nil {
			if info.CurrentLayer != nil {
				job.CurrentLayer = *info.CurrentLayer
			}
			if info.TotalLayer != nil {
				job.TotalLayer = *info.TotalLayer
			}
		}
	}
	// Progress: prefer virtual_sdcard (file position), fall back to display_status.
	if p, ok := vsd.float("progress"); ok {
		job.Progress = p
	} else if p, ok := disp.float("progress"); ok {
		job.Progress = p
	}
	if job.State == "" {
		job.State = "standby"
	}
	return job
}

func toolheadFrom(state map[string]objFields) *Toolhead {
	th, ok := state["toolhead"]
	if !ok {
		return nil
	}
	out := &Toolhead{
		Position:  th.floats("position"),
		HomedAxes: th.str("homed_axes"),
	}
	out.MaxVelocity, _ = th.float("max_velocity")
	if gm, ok := state["gcode_move"]; ok {
		out.SpeedFactor, _ = gm.float("speed_factor")
		out.ExtrudeFactor, _ = gm.float("extrude_factor")
		out.Speed, _ = gm.float("speed")
		if pos := gm.floats("gcode_position"); len(pos) >= 3 && len(out.Position) < 3 {
			out.Position = pos
		}
	}
	// Part-cooling fan speed for the toolhead card, if a bare `fan` exists.
	if fan, ok := state["fan"]; ok {
		if sp, ok := fan.float("speed"); ok {
			out.FanSpeed = &sp
		}
	}
	return out
}

// prettify turns a Klipper object suffix ("chamber_heater") into a display
// label ("Chamber heater").
func prettify(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "_", " "))
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
