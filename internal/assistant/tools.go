package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"homehub/internal/calendar"
	"homehub/internal/metrics"
	"homehub/internal/netscan"
	"homehub/internal/store"
)

// Access is the calling user's authority, passed into every tool run. The
// closures are bound to the real RBAC engine by the API layer — the
// assistant can never see more than the person asking.
type Access struct {
	UserID       uuid.UUID
	DisplayName  string
	Can          func(resource, level string) bool
	VisibleUnder func(prefix string) bool
}

// Toolbox executes tools against HomeHub's own services. Call performs an
// in-process API request under the calling user's identity (nil disables
// the per-system tools).
type Toolbox struct {
	Store   *store.Store
	Metrics *metrics.Service
	Call    func(ctx context.Context, method, path string, body any) (int, []byte)
}

type toolFunc func(ctx context.Context, access Access, input json.RawMessage) (any, error)

type tool struct {
	def ToolDef
	run toolFunc
}

func obj(properties string, required ...string) json.RawMessage {
	req := "[]"
	if len(required) > 0 {
		b, _ := json.Marshal(required)
		req = string(b)
	}
	return json.RawMessage(`{"type":"object","properties":{` + properties + `},"required":` + req + `}`)
}

func (tb *Toolbox) tools() []tool {
	return []tool{
		{
			def: ToolDef{
				Name:        "list_systems",
				Description: "Every system connected to HomeHub that the user may see: type, name, connection status and the current status message.",
				InputSchema: obj(``),
			},
			run: tb.listSystems,
		},
		{
			def: ToolDef{
				Name:        "list_alerts",
				Description: "Alert rules and their current state (ok, firing, unknown). Use firing_only to check what is burning right now.",
				InputSchema: obj(`"firing_only":{"type":"boolean","description":"Only rules currently firing"}`),
			},
			run: tb.listAlerts,
		},
		{
			def: ToolDef{
				Name:        "list_metrics",
				Description: "The catalog of recorded metric series (resource, metric, unit) the user may read. Call this to discover what query_metric can be asked about.",
				InputSchema: obj(``),
			},
			run: tb.listMetrics,
		},
		{
			def: ToolDef{
				Name:        "query_metric",
				Description: "History of one metric series: min/avg/max and the latest value over the requested window.",
				InputSchema: obj(`"resource":{"type":"string","description":"Series resource, e.g. truenas/<id>/pool/tank"},"metric":{"type":"string"},"minutes":{"type":"integer","description":"Window length, default 60, max 10080"}`, "resource", "metric"),
			},
			run: tb.queryMetric,
		},
		{
			def: ToolDef{
				Name:        "list_signals",
				Description: "The global signal stream: everything HomeHub's monitors currently flag across every system — failures, warnings, notable info, and conditions that recovered on their own. Includes deep detail (log excerpts, entity lists). This is THE place to check what is going on.",
				InputSchema: obj(`"severity":{"type":"string","description":"Filter: info, warning, critical, or recovered"},"limit":{"type":"integer","description":"Max entries, default 25, max 100"}`),
			},
			run: tb.listSignals,
		},
		{
			def: ToolDef{
				Name:        "list_calendars",
				Description: "The calendars configured in HomeHub: local calendars, synced CalDAV accounts, ICS feeds, and trash-collection (Müllabfuhr) schedules — with their kind and color.",
				InputSchema: obj(``),
			},
			run: tb.listCalendars,
		},
		{
			def: ToolDef{
				Name:        "list_calendar_events",
				Description: "Upcoming calendar events across every calendar, recurrence expanded — title, calendar, start/end (local time), all-day flag and location. Use this for questions about the schedule, what's coming up, or the next trash pickup.",
				InputSchema: obj(`"days":{"type":"integer","description":"How many days ahead to look, default 14, max 365"}`),
			},
			run: tb.listCalendarEvents,
		},
		{
			def: ToolDef{
				Name:        "scan_network",
				Description: "Run HomeHub's IP scanner over a range of the local network and return the hosts that are alive — IP, hostname, MAC, vendor, open ports and ping time. Actively probes the LAN (takes a few seconds). With no range it scans the server's own subnet (.1–.254).",
				InputSchema: obj(`"start":{"type":"string","description":"First IPv4 address, e.g. 192.168.20.1 (optional)"},"end":{"type":"string","description":"Last IPv4 address, e.g. 192.168.20.254 (optional)"},"ports":{"type":"array","items":{"type":"integer"},"description":"Ports to probe (optional; defaults to a common-ports set)"}`),
			},
			run: tb.scanNetwork,
		},
		{
			def: ToolDef{
				Name:        "create_calendar_event",
				Description: "Propose creating a calendar event. This does NOT create it immediately — it is queued and the user must confirm. Pass start (and end) as local date-times like \"2026-08-25 10:00\", or a date like \"2026-08-25\" for an all-day event. Tell the user what you're queuing; the confirmation happens in the UI.",
				InputSchema: obj(`"title":{"type":"string"},"start":{"type":"string","description":"Local start, e.g. 2026-08-25 10:00 or 2026-08-25 for all-day"},"end":{"type":"string","description":"Local end (optional; defaults to +1h, or the same day for all-day)"},"allDay":{"type":"boolean"},"location":{"type":"string"},"notes":{"type":"string"},"url":{"type":"string"},"calendar":{"type":"string","description":"Target calendar name (optional; defaults to the first writable one)"},"repeat":{"type":"string","description":"none, daily, weekly, biweekly, monthly, or yearly"}`, "title", "start"),
			},
			run: tb.proposeCreateEvent,
		},
		{
			def: ToolDef{
				Name:        "dismiss_signals",
				Description: "Propose dismissing one or more signals (from list_signals) by their id. This does NOT dismiss them immediately — it is queued and the user must confirm. Use list_signals first to get the ids.",
				InputSchema: obj(`"ids":{"type":"array","items":{"type":"integer"},"description":"Signal ids to dismiss"}`, "ids"),
			},
			run: tb.proposeDismissSignals,
		},
		{
			def: ToolDef{
				Name:        "my_notifications",
				Description: "The user's most recent HomeHub notifications (inbox).",
				InputSchema: obj(`"limit":{"type":"integer","description":"Max entries, default 10, max 20"}`),
			},
			run: tb.myNotifications,
		},
		{
			def: ToolDef{
				Name:        "recent_audit",
				Description: "Recent audit-log entries: who did what, when. Requires audit permission.",
				InputSchema: obj(`"limit":{"type":"integer","description":"Max entries, default 10, max 20"}`),
			},
			run: tb.recentAudit,
		},
		{
			def: ToolDef{
				Name:        "list_workspace_files",
				Description: "The user's saved workspace files from the utilities editor: name, size, last edited. Use read_workspace_file to fetch one's content.",
				InputSchema: obj(``),
			},
			run: tb.listWorkspaceFiles,
		},
		{
			def: ToolDef{
				Name:        "read_workspace_file",
				Description: "Read one of the user's workspace files by its exact name (from list_workspace_files). Content beyond 48 KB is truncated.",
				InputSchema: obj(`"name":{"type":"string","description":"Exact file name including extension"}`, "name"),
			},
			run: tb.readWorkspaceFile,
		},
	}
}

// ---- workspace files (the utilities editor's storage) ----------------------

func (tb *Toolbox) listWorkspaceFiles(ctx context.Context, access Access, _ json.RawMessage) (any, error) {
	rows, err := tb.Store.ListUserFiles(ctx, access.UserID)
	if err != nil {
		return nil, err
	}
	type fileInfo struct {
		Name      string    `json:"name"`
		SizeBytes int64     `json:"sizeBytes"`
		UpdatedAt time.Time `json:"updatedAt"`
	}
	out := make([]fileInfo, 0, len(rows))
	for _, f := range rows {
		if f.IsBinary {
			continue // blobs aren't readable context
		}
		out = append(out, fileInfo{Name: f.Name, SizeBytes: f.Size, UpdatedAt: f.UpdatedAt})
	}
	return out, nil
}

const workspaceReadCap = 48 * 1024

func (tb *Toolbox) readWorkspaceFile(ctx context.Context, access Access, input json.RawMessage) (any, error) {
	var args struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(input, &args); err != nil || strings.TrimSpace(args.Name) == "" {
		return nil, fmt.Errorf("read_workspace_file needs a file name")
	}
	f, err := tb.Store.GetUserFileByName(ctx, access.UserID, strings.TrimSpace(args.Name))
	if err != nil {
		return nil, fmt.Errorf("no workspace file named %q — list_workspace_files shows what exists", args.Name)
	}
	content := f.Content
	truncated := false
	if len(content) > workspaceReadCap {
		content = content[:workspaceReadCap]
		// Back off to a rune boundary so we never hand the model a string
		// that ends mid-character (json.Marshal would emit U+FFFD there).
		for len(content) > 0 && !utf8.RuneStart(content[len(content)-1]) {
			content = content[:len(content)-1]
		}
		truncated = true
	}
	return map[string]any{
		"name":      f.Name,
		"sizeBytes": f.Size,
		"updatedAt": f.UpdatedAt,
		"content":   content,
		"truncated": truncated,
	}, nil
}

// allTools joins the base read tools with the per-system depth/control
// tools (present only when an API bridge was provided).
func (tb *Toolbox) allTools() []tool {
	all := tb.tools()
	if tb.Call != nil {
		all = append(all, tb.systemTools()...)
	}
	return all
}

// Defs lists the tool definitions for the provider.
func (tb *Toolbox) Defs() []ToolDef {
	all := tb.allTools()
	defs := make([]ToolDef, len(all))
	for i, tl := range all {
		defs[i] = tl.def
	}
	return defs
}

// Run executes one named tool. Unknown names and refusals come back as
// readable errors for the model to relay.
// RunTool runs a tool and returns its RAW result (which may be a
// *PendingAction for write tools that need user confirmation).
func (tb *Toolbox) RunTool(ctx context.Context, access Access, name string, input json.RawMessage) (any, error) {
	for _, tl := range tb.allTools() {
		if tl.def.Name == name {
			return tl.run(ctx, access, input)
		}
	}
	return nil, fmt.Errorf("no such tool: %s", name)
}

func (tb *Toolbox) Run(ctx context.Context, access Access, name string, input json.RawMessage) (string, error) {
	out, err := tb.RunTool(ctx, access, name, input)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// ---- tool implementations ---------------------------------------------------

func (tb *Toolbox) listSystems(ctx context.Context, access Access, _ json.RawMessage) (any, error) {
	rows, err := tb.Store.ListIntegrations(ctx)
	if err != nil {
		return nil, err
	}
	type system struct {
		ID            string `json:"id"`
		Type          string `json:"type"`
		Name          string `json:"name"`
		Enabled       bool   `json:"enabled"`
		Status        string `json:"status"`
		StatusMessage string `json:"statusMessage,omitempty"`
	}
	out := []system{}
	for _, row := range rows {
		if !access.VisibleUnder(row.Type + "/" + row.ID.String()) {
			continue
		}
		out = append(out, system{
			ID: row.ID.String(), Type: row.Type, Name: row.Name, Enabled: row.Enabled,
			Status: row.Status, StatusMessage: row.StatusMessage,
		})
	}
	return out, nil
}

func (tb *Toolbox) listAlerts(ctx context.Context, access Access, input json.RawMessage) (any, error) {
	var args struct {
		FiringOnly bool `json:"firing_only"`
	}
	_ = json.Unmarshal(input, &args)
	rules, err := tb.Store.ListAlertRules(ctx)
	if err != nil {
		return nil, err
	}
	type alert struct {
		Name     string `json:"name"`
		Resource string `json:"resource"`
		Metric   string `json:"metric"`
		Severity string `json:"severity"`
		Enabled  bool   `json:"enabled"`
		State    string `json:"state"`
		Since    string `json:"since"`
	}
	out := []alert{}
	for _, rule := range rules {
		if !access.VisibleUnder(rule.Resource) {
			continue
		}
		if args.FiringOnly && rule.State != "firing" {
			continue
		}
		out = append(out, alert{
			Name: rule.Name, Resource: rule.Resource, Metric: rule.Metric,
			Severity: rule.Severity, Enabled: rule.Enabled, State: rule.State,
			Since: rule.StateChangedAt.UTC().Format(time.RFC3339),
		})
	}
	return out, nil
}

func (tb *Toolbox) listMetrics(ctx context.Context, access Access, _ json.RawMessage) (any, error) {
	series, err := tb.Store.ListMetricSeries(ctx)
	if err != nil {
		return nil, err
	}
	type entry struct {
		Resource string `json:"resource"`
		Metric   string `json:"metric"`
		Unit     string `json:"unit,omitempty"`
	}
	out := []entry{}
	for _, info := range series {
		if !access.Can(info.Resource, "view") {
			continue
		}
		out = append(out, entry{Resource: info.Resource, Metric: info.Metric, Unit: info.Unit})
	}
	return out, nil
}

func (tb *Toolbox) queryMetric(ctx context.Context, access Access, input json.RawMessage) (any, error) {
	var args struct {
		Resource string `json:"resource"`
		Metric   string `json:"metric"`
		Minutes  int    `json:"minutes"`
	}
	if err := json.Unmarshal(input, &args); err != nil || args.Resource == "" || args.Metric == "" {
		return nil, fmt.Errorf("resource and metric are required")
	}
	if !access.Can(args.Resource, "view") {
		return nil, fmt.Errorf("you may not view %s", args.Resource)
	}
	if args.Minutes <= 0 {
		args.Minutes = 60
	}
	if args.Minutes > 10080 {
		args.Minutes = 10080
	}
	to := time.Now()
	from := to.Add(-time.Duration(args.Minutes) * time.Minute)
	points, err := tb.Metrics.Query(ctx, args.Resource, args.Metric, from, to)
	if err != nil {
		return nil, err
	}
	if len(points) == 0 {
		return map[string]any{"points": 0, "note": "no data in this window"}, nil
	}
	min, max, sum := points[0].Min, points[0].Max, 0.0
	for _, p := range points {
		if p.Min < min {
			min = p.Min
		}
		if p.Max > max {
			max = p.Max
		}
		sum += p.Avg
	}
	last := points[len(points)-1]
	return map[string]any{
		"points": len(points),
		"from":   from.UTC().Format(time.RFC3339),
		"to":     to.UTC().Format(time.RFC3339),
		"min":    min,
		"avg":    sum / float64(len(points)),
		"max":    max,
		"last":   last.Avg,
		"lastAt": last.TS.UTC().Format(time.RFC3339),
	}, nil
}

func (tb *Toolbox) listCalendars(ctx context.Context, _ Access, _ json.RawMessage) (any, error) {
	cals, err := tb.Store.ListCalendars(ctx)
	if err != nil {
		return nil, err
	}
	type cal struct {
		Name     string `json:"name"`
		Kind     string `json:"kind"`
		Color    string `json:"color"`
		ReadOnly bool   `json:"readOnly"`
	}
	out := make([]cal, 0, len(cals))
	for _, c := range cals {
		out = append(out, cal{Name: c.Name, Kind: c.Kind, Color: c.Color, ReadOnly: c.ReadOnly})
	}
	return out, nil
}

func (tb *Toolbox) listCalendarEvents(ctx context.Context, _ Access, input json.RawMessage) (any, error) {
	var args struct {
		Days int `json:"days"`
	}
	_ = json.Unmarshal(input, &args)
	if args.Days <= 0 {
		args.Days = 14
	}
	if args.Days > 365 {
		args.Days = 365
	}
	from := time.Now()
	to := from.AddDate(0, 0, args.Days)

	cals, err := tb.Store.ListCalendars(ctx)
	if err != nil {
		return nil, err
	}
	if len(cals) == 0 {
		return map[string]any{"windowDays": args.Days, "events": []any{}}, nil
	}
	ids := make([]uuid.UUID, 0, len(cals))
	name := map[uuid.UUID]string{}
	for _, c := range cals {
		ids = append(ids, c.ID)
		name[c.ID] = c.Name
	}
	rows, err := tb.Store.ListCalendarEventsInRange(ctx, ids, from, to)
	if err != nil {
		return nil, err
	}
	instances := calendar.Expand(rows, from, to)

	type ev struct {
		Title    string `json:"title"`
		Calendar string `json:"calendar"`
		Start    string `json:"start"`
		End      string `json:"end"`
		AllDay   bool   `json:"allDay,omitempty"`
		Location string `json:"location,omitempty"`
	}
	out := make([]ev, 0, len(instances))
	for _, i := range instances {
		e := ev{Title: i.Title, Calendar: name[i.CalendarID], Location: i.Location, AllDay: i.AllDay}
		if i.AllDay {
			e.Start = i.Start.Local().Format("Mon 2006-01-02")
			e.End = i.End.Local().Format("Mon 2006-01-02")
		} else {
			e.Start = i.Start.Local().Format("Mon 2006-01-02 15:04")
			e.End = i.End.Local().Format("Mon 2006-01-02 15:04")
		}
		out = append(out, e)
		if len(out) >= 150 {
			break
		}
	}
	return map[string]any{"windowDays": args.Days, "events": out}, nil
}

func (tb *Toolbox) scanNetwork(ctx context.Context, _ Access, input json.RawMessage) (any, error) {
	var args struct {
		Start string `json:"start"`
		End   string `json:"end"`
		Ports []int  `json:"ports"`
	}
	_ = json.Unmarshal(input, &args)

	var start, end net.IP
	if args.Start != "" && args.End != "" {
		start, end = netscan.ParseIP4(args.Start), netscan.ParseIP4(args.End)
	} else {
		nets := netscan.LocalNetworks()
		if len(nets) == 0 {
			return nil, fmt.Errorf("could not detect a local subnet to scan")
		}
		start, end = netscan.ParseIP4(nets[0].Start), netscan.ParseIP4(nets[0].End)
	}
	if start == nil || end == nil {
		return nil, fmt.Errorf("start and end must be IPv4 addresses")
	}
	n := netscan.EnumerateCount(start, end)
	if n == 0 || n > 512 {
		return nil, fmt.Errorf("range must cover 1..512 addresses")
	}

	sctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	type host struct {
		IP        string  `json:"ip"`
		Hostname  string  `json:"hostname,omitempty"`
		MAC       string  `json:"mac,omitempty"`
		Vendor    string  `json:"vendor,omitempty"`
		OpenPorts []int   `json:"openPorts"`
		RttMs     float64 `json:"rttMs,omitempty"`
	}
	var mu sync.Mutex
	var alive []host
	netscan.Scan(sctx, netscan.Options{Start: start, End: end, Ports: args.Ports}, func(r netscan.HostResult) {
		if !r.Alive {
			return
		}
		mu.Lock()
		alive = append(alive, host{IP: r.IP, Hostname: r.Hostname, MAC: r.MAC, Vendor: r.Vendor, OpenPorts: r.OpenPorts, RttMs: r.RttMs})
		mu.Unlock()
	})
	ipVal := func(s string) uint32 {
		v4 := net.ParseIP(s).To4()
		if v4 == nil {
			return 0
		}
		return uint32(v4[0])<<24 | uint32(v4[1])<<16 | uint32(v4[2])<<8 | uint32(v4[3])
	}
	sort.Slice(alive, func(i, j int) bool { return ipVal(alive[i].IP) < ipVal(alive[j].IP) })
	return map[string]any{"scanned": n, "aliveCount": len(alive), "hosts": alive}, nil
}

// PendingAction is what a write tool returns instead of executing: a
// concrete, validated change awaiting the user's confirmation in the UI.
// The turn surfaces it; the /assistant/actions/apply endpoint executes it.
type PendingAction struct {
	Kind    string          `json:"kind"`
	Summary string          `json:"summary"`
	Params  json.RawMessage `json:"params"`
}

// parseWhen accepts a local date-time or a bare date (all-day).
func parseWhen(s string) (t time.Time, dateOnly bool, err error) {
	s = strings.TrimSpace(s)
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02 15:04"} {
		if v, e := time.ParseInLocation(layout, s, time.Local); e == nil {
			return v, false, nil
		}
	}
	if v, e := time.ParseInLocation("2006-01-02", s, time.Local); e == nil {
		return v, true, nil
	}
	return time.Time{}, false, fmt.Errorf("could not read the date/time %q — use e.g. 2026-08-25 10:00", s)
}

func repeatToRRule(repeat string) string {
	switch strings.ToLower(strings.TrimSpace(repeat)) {
	case "daily":
		return "FREQ=DAILY"
	case "weekly":
		return "FREQ=WEEKLY"
	case "biweekly":
		return "FREQ=WEEKLY;INTERVAL=2"
	case "monthly":
		return "FREQ=MONTHLY"
	case "yearly":
		return "FREQ=YEARLY"
	default:
		return ""
	}
}

// calendarCreateParams is the concrete payload the apply endpoint executes.
type calendarCreateParams struct {
	CalendarID   uuid.UUID `json:"calendarId"`
	CalendarName string    `json:"calendarName"`
	Title        string    `json:"title"`
	StartsAt     time.Time `json:"startsAt"`
	EndsAt       time.Time `json:"endsAt"`
	AllDay       bool      `json:"allDay"`
	Location     string    `json:"location,omitempty"`
	Description  string    `json:"description,omitempty"`
	URL          string    `json:"url,omitempty"`
	RRule        string    `json:"rrule,omitempty"`
}

func (tb *Toolbox) proposeCreateEvent(ctx context.Context, _ Access, input json.RawMessage) (any, error) {
	var args struct {
		Title, Start, End, Location, Notes, URL, Calendar, Repeat string
		AllDay                                                    *bool
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	if strings.TrimSpace(args.Title) == "" || strings.TrimSpace(args.Start) == "" {
		return nil, fmt.Errorf("a title and a start time are required")
	}
	start, dateOnly, err := parseWhen(args.Start)
	if err != nil {
		return nil, err
	}
	allDay := dateOnly
	if args.AllDay != nil {
		allDay = *args.AllDay
	}
	var end time.Time
	if strings.TrimSpace(args.End) != "" {
		end, _, err = parseWhen(args.End)
		if err != nil {
			return nil, err
		}
	} else if allDay {
		end = start.Add(24 * time.Hour)
	} else {
		end = start.Add(time.Hour)
	}
	if !end.After(start) {
		if allDay {
			end = start.Add(24 * time.Hour)
		} else {
			end = start.Add(time.Hour)
		}
	}

	// Resolve the target calendar: named, else the first writable local one.
	cals, err := tb.Store.ListCalendars(ctx)
	if err != nil {
		return nil, err
	}
	var target *store.Calendar
	for i := range cals {
		c := &cals[i]
		if c.ReadOnly {
			continue
		}
		if args.Calendar != "" {
			if strings.EqualFold(c.Name, strings.TrimSpace(args.Calendar)) {
				target = c
				break
			}
			continue
		}
		if target == nil {
			target = c
		}
	}
	if target == nil {
		if args.Calendar != "" {
			return nil, fmt.Errorf("no writable calendar named %q — check list_calendars", args.Calendar)
		}
		return nil, fmt.Errorf("there is no writable calendar yet — create a local calendar first")
	}

	params := calendarCreateParams{
		CalendarID: target.ID, CalendarName: target.Name, Title: strings.TrimSpace(args.Title),
		StartsAt: start, EndsAt: end, AllDay: allDay,
		Location: strings.TrimSpace(args.Location), Description: strings.TrimSpace(args.Notes),
		URL: strings.TrimSpace(args.URL), RRule: repeatToRRule(args.Repeat),
	}
	raw, _ := json.Marshal(params)
	when := start.Format("Mon Jan 2, 15:04")
	if allDay {
		when = start.Format("Mon Jan 2") + " (all day)"
	}
	summary := fmt.Sprintf("Create “%s” on %s in %s", params.Title, when, target.Name)
	if params.RRule != "" {
		summary += " (repeating)"
	}
	return &PendingAction{Kind: "calendar.create", Summary: summary, Params: raw}, nil
}

func (tb *Toolbox) proposeDismissSignals(ctx context.Context, _ Access, input json.RawMessage) (any, error) {
	var args struct {
		Ids []int64 `json:"ids"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	if len(args.Ids) == 0 {
		return nil, fmt.Errorf("no signal ids given")
	}
	// Summarize with the signals' titles so the confirmation is meaningful.
	rows, _ := tb.Store.ListSignals(ctx, 500)
	titleByID := map[int64]string{}
	for _, r := range rows {
		titleByID[r.ID] = r.Title
	}
	names := make([]string, 0, len(args.Ids))
	for _, id := range args.Ids {
		if t := titleByID[id]; t != "" {
			names = append(names, t)
		}
	}
	raw, _ := json.Marshal(map[string]any{"ids": args.Ids})
	summary := fmt.Sprintf("Dismiss %d signal(s)", len(args.Ids))
	if len(names) > 0 {
		summary += ": " + strings.Join(names, ", ")
	}
	return &PendingAction{Kind: "signals.dismiss", Summary: summary, Params: raw}, nil
}

func (tb *Toolbox) listSignals(ctx context.Context, _ Access, input json.RawMessage) (any, error) {
	var args struct {
		Severity string `json:"severity"`
		Limit    int    `json:"limit"`
	}
	_ = json.Unmarshal(input, &args)
	if args.Limit <= 0 {
		args.Limit = 25
	}
	if args.Limit > 100 {
		args.Limit = 100
	}
	rows, err := tb.Store.ListSignals(ctx, 500)
	if err != nil {
		return nil, err
	}
	type sig struct {
		ID       int64  `json:"id"`
		Severity string `json:"severity"`
		Source   string `json:"source"`
		Title    string `json:"title"`
		Body     string `json:"body,omitempty"`
		Count    int    `json:"count,omitempty"`
		Since    string `json:"since"`
		LastSeen string `json:"lastSeen"`
		Detail   any    `json:"detail,omitempty"`
	}
	out := make([]sig, 0, args.Limit)
	for _, row := range rows {
		if args.Severity != "" && row.Severity != args.Severity {
			continue
		}
		var detail any
		if len(row.Detail) > 0 {
			_ = json.Unmarshal(row.Detail, &detail)
		}
		out = append(out, sig{
			ID:       row.ID,
			Severity: row.Severity,
			Source:   row.SourceName,
			Title:    row.Title,
			Body:     row.Body,
			Count:    row.Count,
			Since:    row.FirstSeen.UTC().Format(time.RFC3339),
			LastSeen: row.LastSeen.UTC().Format(time.RFC3339),
			Detail:   detail,
		})
		if len(out) >= args.Limit {
			break
		}
	}
	return map[string]any{"signals": out, "totalLive": len(rows)}, nil
}

func (tb *Toolbox) myNotifications(ctx context.Context, access Access, input json.RawMessage) (any, error) {
	var args struct {
		Limit int `json:"limit"`
	}
	_ = json.Unmarshal(input, &args)
	if args.Limit <= 0 {
		args.Limit = 10
	}
	if args.Limit > 20 {
		args.Limit = 20
	}
	rows, err := tb.Store.ListNotifications(ctx, access.UserID, false, nil, args.Limit)
	if err != nil {
		return nil, err
	}
	type note struct {
		Severity string `json:"severity"`
		Title    string `json:"title"`
		Body     string `json:"body,omitempty"`
		At       string `json:"at"`
		Read     bool   `json:"read"`
	}
	out := make([]note, 0, len(rows))
	for _, row := range rows {
		out = append(out, note{
			Severity: row.Severity, Title: row.Title, Body: row.Body,
			At: row.TS.UTC().Format(time.RFC3339), Read: row.ReadAt != nil,
		})
	}
	return out, nil
}

func (tb *Toolbox) recentAudit(ctx context.Context, access Access, input json.RawMessage) (any, error) {
	if !access.Can("system/audit", "view") {
		return nil, fmt.Errorf("you may not read the audit log")
	}
	var args struct {
		Limit int `json:"limit"`
	}
	_ = json.Unmarshal(input, &args)
	if args.Limit <= 0 {
		args.Limit = 10
	}
	if args.Limit > 20 {
		args.Limit = 20
	}
	rows, err := tb.Store.ListAudit(ctx, store.AuditFilter{Limit: args.Limit})
	if err != nil {
		return nil, err
	}
	type entry struct {
		Actor    string `json:"actor"`
		Action   string `json:"action"`
		Resource string `json:"resource,omitempty"`
		Outcome  string `json:"outcome"`
		At       string `json:"at"`
	}
	out := make([]entry, 0, len(rows))
	for _, row := range rows {
		out = append(out, entry{
			Actor: row.ActorName, Action: row.Action, Resource: row.Resource,
			Outcome: row.Outcome, At: row.TS.UTC().Format(time.RFC3339),
		})
	}
	return out, nil
}
