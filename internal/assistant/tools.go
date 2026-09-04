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

	"github.com/TheOriUHD/homehub/internal/calendar"
	"github.com/TheOriUHD/homehub/internal/metrics"
	"github.com/TheOriUHD/homehub/internal/netscan"
	"github.com/TheOriUHD/homehub/internal/store"
)

// Access is the calling user's authority, passed into every tool run. The
// closures are bound to the real RBAC engine by the API layer — the
// assistant can never see more than the person asking.
type Access struct {
	UserID       uuid.UUID
	DisplayName  string
	Can          func(resource, level string) bool
	VisibleUnder func(prefix string) bool
	// Route is the Auto rule that picked this turn's model, stored with the
	// answer so the reader can see what decided.
	Route string
	// Channel scopes the conversation history: "web" or "telegram" — the two
	// surfaces keep fully separate threads. Conversation further scopes the
	// web channel to one app session; Telegram uses "".
	Channel      string
	Conversation string
	// ContextLines ride in the per-user half of the system prompt, after the
	// cache breakpoint: facts the surface wants the model to have without a
	// tool call — on Telegram, what it sent the person recently.
	ContextLines []string
}

// Toolbox executes tools against HomeHub's own services. Call performs an
// in-process API request under the calling user's identity (nil disables
// the per-system tools).
type Toolbox struct {
	Store   *store.Store
	Metrics *metrics.Service
	Call    func(ctx context.Context, method, path string, body any) (int, []byte)
	// SSHRun executes one command on a registry host, returning its output
	// and exit code. Supplied by the API layer (which owns the vault); nil
	// disables the SSH tools.
	SSHRun func(ctx context.Context, hostID, command string) (string, int, error)
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
	base := []tool{
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
				InputSchema: obj(`"start":{"type":"string","description":"First IPv4 address, e.g. 192.168.10.1 (optional)"},"end":{"type":"string","description":"Last IPv4 address, e.g. 192.168.10.254 (optional)"},"ports":{"type":"array","items":{"type":"integer"},"description":"Ports to probe (optional; defaults to a common-ports set)"}`),
			},
			run: tb.scanNetwork,
		},
		{
			def: ToolDef{
				Name:        "create_calendar_event",
				Description: "Create a calendar event immediately (adding an event is non-destructive, so it does NOT need confirmation). Pass start (and end) as local date-times like \"2026-08-25 10:00\", or a date like \"2026-08-25\" for an all-day event. Confirm to the user what you created afterwards.",
				InputSchema: obj(`"title":{"type":"string"},"start":{"type":"string","description":"Local start, e.g. 2026-08-25 10:00 or 2026-08-25 for all-day"},"end":{"type":"string","description":"Local end (optional; defaults to +1h, or the same day for all-day)"},"allDay":{"type":"boolean"},"location":{"type":"string"},"notes":{"type":"string"},"url":{"type":"string"},"calendar":{"type":"string","description":"Target calendar name (optional; defaults to the first writable one)"},"repeat":{"type":"string","description":"none, daily, weekly, biweekly, monthly, or yearly"}`, "title", "start"),
			},
			run: tb.createEventDirect,
		},
		{
			def: ToolDef{
				Name:        "delete_calendar_event",
				Description: "Propose deleting a calendar event (destructive — it is QUEUED and the user must confirm). eventId comes from list_calendar_events. For a repeating event set scope: 'this' (only the clicked occurrence), 'following' (this and all later ones) or 'all' (the whole series); pass occurrenceStart (from list_calendar_events) for this/following.",
				InputSchema: obj(`"eventId":{"type":"string","description":"Event id from list_calendar_events"},"scope":{"type":"string","description":"this, following, or all (default all)"},"occurrenceStart":{"type":"string","description":"The occurrenceStart of the instance (for this/following)"}`, "eventId"),
			},
			run: tb.proposeDeleteEvent,
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
				Name:        "create_calendar",
				Description: "Propose creating a new calendar. This does NOT create it immediately — it is queued and the user must confirm. kind is 'local' (default; a plain editable calendar), 'ics' (a read-only subscribed feed — needs a url), or 'trash' (a Müllabfuhr pickup schedule — needs a url). CalDAV accounts must be added in the UI because they need a password.",
				InputSchema: obj(`"name":{"type":"string"},"color":{"type":"string","description":"Hex color like #4f46e5 (optional)"},"kind":{"type":"string","description":"local, ics, or trash — default local"},"url":{"type":"string","description":"Feed URL (required for ics and trash)"}`, "name"),
			},
			run: tb.proposeCreateCalendar,
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
				Name:        "sent_notifications",
				Description: "The alerts HomeHub pushed to the owner's phone (Telegram), newest first, each with whether its condition is still firing or has cleared. Use this whenever the user refers to an alert or to something you told them — 'that printer', 'the thing from earlier', 'is it still down' — instead of answering from memory.",
				InputSchema: obj(`"hours":{"type":"integer","description":"How far back to look, default 48, max 720"},"contains":{"type":"string","description":"Only alerts mentioning this text (system name, word from the message)"},"limit":{"type":"integer","description":"Max entries, default 15, max 40"}`),
			},
			run: tb.sentNotifications,
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
	if tb.SSHRun != nil {
		base = append(base, tb.sshTools()...)
	}
	return base
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
		EventID         string `json:"eventId"`
		CalendarID      string `json:"calendarId"`
		Title           string `json:"title"`
		Calendar        string `json:"calendar"`
		Start           string `json:"start"`
		End             string `json:"end"`
		AllDay          bool   `json:"allDay,omitempty"`
		Location        string `json:"location,omitempty"`
		Recurring       bool   `json:"recurring,omitempty"`
		OccurrenceStart string `json:"occurrenceStart,omitempty"`
	}
	out := make([]ev, 0, len(instances))
	for _, i := range instances {
		e := ev{
			EventID: i.EventID.String(), CalendarID: i.CalendarID.String(),
			Title: i.Title, Calendar: name[i.CalendarID], Location: i.Location, AllDay: i.AllDay,
			Recurring: i.Recurring, OccurrenceStart: i.OccurrenceStart.UTC().Format(time.RFC3339),
		}
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

func (tb *Toolbox) createEventDirect(ctx context.Context, _ Access, input json.RawMessage) (any, error) {
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

	// Adding an event is non-destructive — create it directly (no confirm)
	// through the same API the UI uses, under the caller's identity.
	if tb.Call == nil {
		return nil, fmt.Errorf("calendar is not reachable from the assistant here")
	}
	body := map[string]any{
		"calendarId": target.ID.String(),
		"event": map[string]any{
			"title":       strings.TrimSpace(args.Title),
			"startsAt":    start.Format(time.RFC3339),
			"endsAt":      end.Format(time.RFC3339),
			"allDay":      allDay,
			"location":    strings.TrimSpace(args.Location),
			"description": strings.TrimSpace(args.Notes),
			"url":         strings.TrimSpace(args.URL),
			"rrule":       repeatToRRule(args.Repeat),
		},
	}
	status, resp := tb.Call(ctx, "POST", "/calendar/events", body)
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("could not create the event: %s", string(resp))
	}
	when := start.Format("Mon Jan 2, 15:04")
	if allDay {
		when = start.Format("Mon Jan 2") + " (all day)"
	}
	return map[string]any{
		"created":  true,
		"title":    strings.TrimSpace(args.Title),
		"calendar": target.Name,
		"when":     when,
	}, nil
}

func (tb *Toolbox) proposeDeleteEvent(ctx context.Context, _ Access, input json.RawMessage) (any, error) {
	var args struct {
		EventID, Scope, OccurrenceStart string
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	id, err := uuid.Parse(strings.TrimSpace(args.EventID))
	if err != nil {
		return nil, fmt.Errorf("eventId must be an id from list_calendar_events")
	}
	ev, err := tb.Store.GetCalendarEvent(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("no such event — list_calendar_events shows what exists")
	}
	scope := strings.ToLower(strings.TrimSpace(args.Scope))
	if scope == "" {
		scope = "all"
	}
	summary := fmt.Sprintf("Delete “%s”", ev.Title)
	if ev.RRule != "" || ev.RecurrenceID != nil {
		switch scope {
		case "this":
			summary += " (this occurrence only)"
		case "following":
			summary += " (this and all following)"
		default:
			summary += " (the whole repeating series)"
		}
	}
	raw, _ := json.Marshal(map[string]any{"eventId": id.String(), "scope": scope, "occurrenceStart": strings.TrimSpace(args.OccurrenceStart)})
	return &PendingAction{Kind: "calendar.delete_event", Summary: summary, Params: raw}, nil
}

func (tb *Toolbox) proposeCreateCalendar(_ context.Context, _ Access, input json.RawMessage) (any, error) {
	var args struct {
		Name, Color, Kind, URL string
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(args.Name)
	if name == "" {
		return nil, fmt.Errorf("a calendar name is required")
	}
	kind := strings.ToLower(strings.TrimSpace(args.Kind))
	if kind == "" {
		kind = "local"
	}
	url := strings.TrimSpace(args.URL)
	var summary string
	switch kind {
	case "local":
		summary = fmt.Sprintf("Create a local calendar “%s”", name)
	case "ics":
		if url == "" {
			return nil, fmt.Errorf("an ICS calendar needs a feed url")
		}
		summary = fmt.Sprintf("Subscribe to ICS calendar “%s”", name)
	case "trash":
		if url == "" {
			return nil, fmt.Errorf("a trash-pickup calendar needs its schedule url")
		}
		summary = fmt.Sprintf("Add trash-pickup calendar “%s”", name)
	case "caldav":
		return nil, fmt.Errorf("CalDAV calendars must be added in the UI — they need a password I can't handle")
	default:
		return nil, fmt.Errorf("unknown calendar kind %q (use local, ics, or trash)", kind)
	}
	color := strings.TrimSpace(args.Color)
	if color == "" {
		color = "#4f46e5"
	}
	raw, _ := json.Marshal(map[string]any{"name": name, "color": color, "kind": kind, "url": url})
	return &PendingAction{Kind: "calendar.create_calendar", Summary: summary, Params: raw}, nil
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

func (tb *Toolbox) listSignals(ctx context.Context, access Access, input json.RawMessage) (any, error) {
	// The signals surface can be hidden for this account (per-user or by role);
	// the assistant respects it — it must not read out what the UI hides.
	if hidden, err := tb.Store.SignalsHiddenForUser(ctx, access.UserID); err == nil && hidden {
		return map[string]any{"signals": []any{}, "note": "The signals stream is not available for your account."}, nil
	}
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

func (tb *Toolbox) sentNotifications(ctx context.Context, access Access, input json.RawMessage) (any, error) {
	// Pushed alerts are the signals surface on a phone; if that is hidden for
	// this account, the assistant must not read it out either.
	if hidden, err := tb.Store.SignalsHiddenForUser(ctx, access.UserID); err == nil && hidden {
		return map[string]any{"sent": []any{}, "note": "The signals stream is not available for your account."}, nil
	}
	var args struct {
		Hours    int    `json:"hours"`
		Contains string `json:"contains"`
		Limit    int    `json:"limit"`
	}
	_ = json.Unmarshal(input, &args)
	if args.Hours <= 0 {
		args.Hours = 48
	}
	if args.Hours > 720 {
		args.Hours = 720
	}
	if args.Limit <= 0 {
		args.Limit = 15
	}
	if args.Limit > 40 {
		args.Limit = 40
	}
	rows, err := tb.Store.ListSentNotices(ctx, time.Now().Add(-time.Duration(args.Hours)*time.Hour), strings.TrimSpace(args.Contains), args.Limit)
	if err != nil {
		return nil, err
	}
	type sent struct {
		SentAt   string `json:"sentAt"`
		Event    string `json:"event"`
		Severity string `json:"severity"`
		Source   string `json:"source,omitempty"`
		Title    string `json:"title"`
		Text     string `json:"text"`
		// State is the condition's standing NOW, not when the message went
		// out — the whole reason to look here rather than remember.
		State string `json:"stateNow"`
	}
	out := make([]sent, 0, len(rows))
	for _, r := range rows {
		state := "no longer tracked"
		if last, lerr := tb.Store.LatestSignalByKey(ctx, r.Key); lerr == nil && last != nil {
			if last.ResolvedAt == nil && last.DismissedAt == nil {
				state = fmt.Sprintf("still firing (%s) since %s", last.Severity, last.FirstSeen.Local().Format("Mon 2 Jan 15:04"))
			} else if last.ResolvedAt != nil {
				state = "cleared " + last.ResolvedAt.Local().Format("Mon 2 Jan 15:04")
			} else if last.DismissedAt != nil {
				state = "dismissed by the user " + last.DismissedAt.Local().Format("Mon 2 Jan 15:04")
			}
		}
		out = append(out, sent{
			SentAt: r.SentAt.Local().Format("Mon 2 Jan 15:04"), Event: r.Event, Severity: r.Severity,
			Source: r.SourceName, Title: r.Title, Text: r.Text, State: state,
		})
	}
	return map[string]any{"sent": out, "note": "Times are local. stateNow is the condition's standing at this moment."}, nil
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
