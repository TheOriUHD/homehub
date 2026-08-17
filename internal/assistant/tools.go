package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"homehub/internal/metrics"
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
	}
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
func (tb *Toolbox) Run(ctx context.Context, access Access, name string, input json.RawMessage) (string, error) {
	for _, tl := range tb.allTools() {
		if tl.def.Name != name {
			continue
		}
		out, err := tl.run(ctx, access, input)
		if err != nil {
			return "", err
		}
		raw, err := json.Marshal(out)
		if err != nil {
			return "", err
		}
		return string(raw), nil
	}
	return "", fmt.Errorf("no such tool: %s", name)
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
