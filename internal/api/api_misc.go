package api

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"io"
	"strconv"
	"time"

	"github.com/google/uuid"

	"homehub/internal/httpctx"
	"homehub/internal/rbac"
	"homehub/internal/store"
	"homehub/internal/version"
)

// ---- audit -----------------------------------------------------------------

func auditFilterFrom(actorID *uuid.UUID, action, resource, outcome *string, from, to *time.Time, before *int64, limit *int) store.AuditFilter {
	f := store.AuditFilter{}
	if actorID != nil {
		f.ActorID = actorID
	}
	if action != nil {
		f.Action = *action
	}
	if resource != nil {
		f.Resource = *resource
	}
	if outcome != nil {
		f.Outcome = *outcome
	}
	f.From = from
	f.To = to
	f.BeforeID = before
	if limit != nil {
		f.Limit = *limit
	}
	return f
}

func (s *Server) ListAudit(ctx context.Context, req ListAuditRequestObject) (ListAuditResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResAudit, rbac.LevelView) {
		return ListAudit403JSONResponse{forbidden()}, nil
	}
	var outcome *string
	if req.Params.Outcome != nil {
		o := string(*req.Params.Outcome)
		outcome = &o
	}
	filter := auditFilterFrom(req.Params.ActorId, req.Params.Action, req.Params.Resource, outcome,
		req.Params.From, req.Params.To, req.Params.Before, req.Params.Limit)
	entries, err := s.store.ListAudit(ctx, filter)
	if err != nil {
		return nil, err
	}

	out := make([]AuditEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, toAPIAuditEntry(e))
	}
	resp := ListAudit200JSONResponse{Entries: out}
	limit := filter.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if len(entries) == limit {
		next := entries[len(entries)-1].ID
		resp.NextBefore = &next
	}
	return resp, nil
}

func toAPIAuditEntry(e store.AuditEntry) AuditEntry {
	entry := AuditEntry{
		Id:        e.ID,
		Ts:        e.TS,
		ActorKind: AuditEntryActorKind(e.ActorKind),
		ActorId:   e.ActorID,
		ActorName: e.ActorName,
		Action:    e.Action,
		Resource:  e.Resource,
		Outcome:   AuditEntryOutcome(e.Outcome),
		Ip:        e.IP,
	}
	if len(e.Detail) > 0 {
		var detail map[string]any
		if err := json.Unmarshal(e.Detail, &detail); err == nil {
			entry.Detail = &detail
		}
	}
	return entry
}

func (s *Server) ExportAudit(ctx context.Context, req ExportAuditRequestObject) (ExportAuditResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResAudit, rbac.LevelView) {
		return ExportAudit403JSONResponse{forbidden()}, nil
	}
	var outcome *string
	if req.Params.Outcome != nil {
		o := string(*req.Params.Outcome)
		outcome = &o
	}
	filter := auditFilterFrom(req.Params.ActorId, req.Params.Action, req.Params.Resource, outcome,
		req.Params.From, req.Params.To, nil, nil)

	s.audit.Log(ctx, actorFor(id), "audit.export", ResAudit, "success", nil)

	pr, pw := io.Pipe()
	go func() {
		w := csv.NewWriter(pw)
		_ = w.Write([]string{"id", "timestamp", "actor_kind", "actor", "action", "resource", "outcome", "ip", "detail"})
		exportFilter := filter
		exportFilter.Limit = 500
		for {
			entries, err := s.store.ListAudit(ctx, exportFilter)
			if err != nil {
				pw.CloseWithError(err)
				return
			}
			for _, e := range entries {
				_ = w.Write([]string{
					strconv.FormatInt(e.ID, 10),
					e.TS.UTC().Format(time.RFC3339),
					e.ActorKind, e.ActorName, e.Action, e.Resource, e.Outcome, e.IP,
					string(e.Detail),
				})
			}
			if len(entries) < exportFilter.Limit {
				break
			}
			last := entries[len(entries)-1].ID
			exportFilter.BeforeID = &last
		}
		w.Flush()
		pw.CloseWithError(w.Error())
	}()

	return ExportAudit200TextcsvResponse{Body: pr, ContentLength: 0}, nil
}

// ---- notifications ---------------------------------------------------------

func (s *Server) ListNotifications(ctx context.Context, req ListNotificationsRequestObject) (ListNotificationsResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	unread := req.Params.Unread != nil && *req.Params.Unread
	limit := 50
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	items, err := s.store.ListNotifications(ctx, id.User.ID, unread, req.Params.Before, limit)
	if err != nil {
		return nil, err
	}
	unreadCount, err := s.store.UnreadNotificationCount(ctx, id.User.ID)
	if err != nil {
		return nil, err
	}
	out := make([]Notification, 0, len(items))
	for _, n := range items {
		out = append(out, Notification{
			Id:        n.ID,
			Ts:        n.TS,
			Severity:  NotificationSeverity(n.Severity),
			Title:     n.Title,
			Body:      n.Body,
			Resource:  n.Resource,
			EventType: n.EventType,
			Read:      n.ReadAt != nil,
		})
	}
	resp := ListNotifications200JSONResponse{Notifications: out, UnreadCount: unreadCount}
	if len(items) == limit {
		next := items[len(items)-1].ID
		resp.NextBefore = &next
	}
	return resp, nil
}

func (s *Server) MarkNotificationsRead(ctx context.Context, req MarkNotificationsReadRequestObject) (MarkNotificationsReadResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if req.Body.All != nil && *req.Body.All {
		if err := s.store.MarkAllNotificationsRead(ctx, id.User.ID); err != nil {
			return nil, err
		}
	} else if req.Body.Ids != nil && len(*req.Body.Ids) > 0 {
		if err := s.store.MarkNotificationsRead(ctx, id.User.ID, *req.Body.Ids); err != nil {
			return nil, err
		}
	}
	unreadCount, err := s.store.UnreadNotificationCount(ctx, id.User.ID)
	if err != nil {
		return nil, err
	}
	return MarkNotificationsRead200JSONResponse{UnreadCount: unreadCount}, nil
}

// ---- health ----------------------------------------------------------------

func (s *Server) GetHealth(ctx context.Context, _ GetHealthRequestObject) (GetHealthResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResHealth, rbac.LevelView) {
		return GetHealth403JSONResponse{forbidden()}, nil
	}

	report := HealthReport{
		Version:          version.Version,
		UptimeSeconds:    int64(time.Since(s.started).Seconds()),
		WebsocketClients: s.hub.ClientCount(),
	}
	// Integration health joins this report in phase 2; the field is present
	// (and empty) so the contract is stable.
	report.Integrations = make([]struct {
		Id        uuid.UUID                      `json:"id"`
		LastError *string                        `json:"lastError,omitempty"`
		Name      string                         `json:"name"`
		Status    HealthReportIntegrationsStatus `json:"status"`
		Type      string                         `json:"type"`
	}, 0)

	dbStart := time.Now()
	var sizeBytes int64
	err := s.store.Pool().QueryRow(ctx, `select pg_database_size(current_database())`).Scan(&sizeBytes)
	report.Database.LatencyMs = float32(time.Since(dbStart).Seconds() * 1000)
	report.Database.Reachable = err == nil
	report.Database.SizeBytes = sizeBytes

	if jh, err := s.jobs.Health(ctx); err == nil {
		report.Jobs.Workers = jh.Workers
		report.Jobs.Active = jh.ActiveJobs
		report.Jobs.Pending = jh.Pending
		report.Jobs.Running = jh.Running
		report.Jobs.FailedRecent = jh.FailedRecent
	}

	return GetHealth200JSONResponse(report), nil
}

// ---- settings --------------------------------------------------------------

func (s *Server) GetSettings(ctx context.Context, _ GetSettingsRequestObject) (GetSettingsResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResSettings, rbac.LevelView) {
		return GetSettings403JSONResponse{forbidden()}, nil
	}
	return GetSettings200JSONResponse(AppSettings{
		AppUrl:          s.settings.AppURL(ctx),
		SessionTtlHours: s.settings.SessionTTLHours(ctx),
	}), nil
}

func (s *Server) UpdateSettings(ctx context.Context, req UpdateSettingsRequestObject) (UpdateSettingsResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResSettings, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "settings.update", ResSettings)
		return UpdateSettings403JSONResponse{forbidden()}, nil
	}
	changes := map[string]any{}
	if req.Body.AppUrl != nil {
		if err := s.settings.SetAppURL(ctx, *req.Body.AppUrl); err != nil {
			return UpdateSettings400JSONResponse{BadRequestJSONResponse(errBody("invalid_app_url", err.Error()))}, nil
		}
		changes["appUrl"] = *req.Body.AppUrl
	}
	if req.Body.SessionTtlHours != nil {
		if err := s.settings.SetSessionTTLHours(ctx, *req.Body.SessionTtlHours); err != nil {
			return UpdateSettings400JSONResponse{BadRequestJSONResponse(errBody("invalid_session_ttl", err.Error()))}, nil
		}
		changes["sessionTtlHours"] = *req.Body.SessionTtlHours
	}
	s.audit.Log(ctx, actorFor(id), "settings.update", ResSettings, "success", changes)
	return UpdateSettings200JSONResponse(AppSettings{
		AppUrl:          s.settings.AppURL(ctx),
		SessionTtlHours: s.settings.SessionTTLHours(ctx),
	}), nil
}
