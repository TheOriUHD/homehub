package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/go-chi/chi/v5"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/TheOriUHD/homehub/internal/calendar"
	"github.com/TheOriUHD/homehub/internal/httpctx"
	"github.com/TheOriUHD/homehub/internal/store"
)

// Calendar: shared household calendars — local, CalDAV (two-way), ICS feeds
// and trash-collection schedules. Like signals, one homelab, one truth: any
// signed-in user reads; writes go through the calendar service (which
// enforces read-only kinds).

func (s *Server) SetCalendar(svc *calendar.Service) { s.calendar = svc }

// Live topics. calendarTopic is broadcast: a calendar belongs to the house,
// not to whoever happened to change it, so every open calendar refreshes
// whether the edit came from another tab, the agent, or a CalDAV sync.
//
// agentTopic is per-user and carries whatever the agent just did — see
// announceSelfChange in selfcall.go.
const (
	calendarTopic = "calendar"
	agentTopic    = "agent"
)

// calendarChanged nudges every open calendar. Called after a write rather than
// inside the calendar service, which has no hub and should not grow one.
func (s *Server) calendarChanged() {
	if s.hub != nil {
		s.hub.Publish(calendarTopic, map[string]any{"event": "changed"})
	}
}

func toAPICalendar(c *store.Calendar) Calendar {
	out := Calendar{
		Id:           c.ID,
		Name:         c.Name,
		Color:        c.Color,
		Kind:         CalendarKind(c.Kind),
		Url:          c.URL,
		ReadOnly:     c.ReadOnly,
		LastSyncedAt: c.LastSyncedAt,
		CreatedAt:    c.CreatedAt,
	}
	if c.SyncError != "" {
		msg := c.SyncError
		out.SyncError = &msg
	}
	if len(c.Settings) > 0 {
		var v any
		if err := json.Unmarshal(c.Settings, &v); err == nil {
			out.Settings = v
		}
	}
	return out
}

func toAPIInstance(i calendar.Instance, rrule string) CalendarInstance {
	out := CalendarInstance{
		EventId:         i.EventID,
		CalendarId:      i.CalendarID,
		Uid:             i.UID,
		Title:           i.Title,
		Start:           i.Start,
		End:             i.End,
		AllDay:          i.AllDay,
		Recurring:       i.Recurring,
		OccurrenceStart: i.OccurrenceStart,
	}
	setOpt := func(dst **string, v string) {
		if v != "" {
			s := v
			*dst = &s
		}
	}
	setOpt(&out.Location, i.Location)
	setOpt(&out.Description, i.Description)
	setOpt(&out.Url, i.URL)
	setOpt(&out.Rrule, rrule)
	return out
}

func (s *Server) ListCalendars(ctx context.Context, _ ListCalendarsRequestObject) (ListCalendarsResponseObject, error) {
	if httpctx.IdentityFrom(ctx) == nil {
		return nil, ErrUnauthenticated
	}
	rows, err := s.store.ListCalendars(ctx)
	if err != nil {
		return nil, err
	}
	out := make(ListCalendars200JSONResponse, 0, len(rows))
	for i := range rows {
		out = append(out, toAPICalendar(&rows[i]))
	}
	return out, nil
}

func (s *Server) CreateCalendar(ctx context.Context, req CreateCalendarRequestObject) (CreateCalendarResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	b := req.Body
	kind := string(b.Kind)
	color := "#4f46e5"
	if b.Color != nil && *b.Color != "" {
		color = *b.Color
	}
	var settings json.RawMessage
	if b.Settings != nil {
		if raw, err := json.Marshal(b.Settings); err == nil {
			settings = raw
		}
	}
	row := store.Calendar{
		Name:     b.Name,
		Color:    color,
		Kind:     kind,
		ReadOnly: kind == calendar.KindICS || kind == calendar.KindTrash,
	}
	if b.Url != nil {
		row.URL = *b.Url
	}
	if b.Username != nil {
		row.Username = *b.Username
	}
	row.Settings = settings

	switch kind {
	case calendar.KindLocal:
	case calendar.KindCalDAV:
		if row.URL == "" {
			return CreateCalendar400JSONResponse{BadRequestJSONResponse(errBody("bad_url", "a CalDAV calendar needs its collection URL"))}, nil
		}
	case calendar.KindICS, calendar.KindTrash:
		if row.URL == "" && (b.IcsContent == nil || *b.IcsContent == "") {
			return CreateCalendar400JSONResponse{BadRequestJSONResponse(errBody("bad_source", "provide a feed URL or an ICS file"))}, nil
		}
	default:
		return CreateCalendar400JSONResponse{BadRequestJSONResponse(errBody("bad_kind", "unknown calendar kind"))}, nil
	}

	created, err := s.store.CreateCalendar(ctx, row)
	if err != nil {
		return nil, err
	}
	// CalDAV password → vault.
	if kind == calendar.KindCalDAV && b.Password != nil && *b.Password != "" {
		if err := s.vault.Seal(ctx, calendar.VaultScope(created.ID), calendar.VaultKeyPassword, []byte(*b.Password)); err != nil {
			_, _ = s.store.DeleteCalendar(ctx, created.ID)
			return nil, err
		}
	}
	// Inline ICS import parses immediately.
	if (kind == calendar.KindICS || kind == calendar.KindTrash) && b.IcsContent != nil && *b.IcsContent != "" {
		events, perr := calendar.ParseICS(created.ID, []byte(*b.IcsContent), "", "")
		if perr != nil {
			_, _ = s.store.DeleteCalendar(ctx, created.ID)
			return CreateCalendar400JSONResponse{BadRequestJSONResponse(errBody("bad_ics", "that file is not parseable iCalendar"))}, nil
		}
		if err := s.store.ReplaceCalendarEvents(ctx, created.ID, events); err != nil {
			return nil, err
		}
	}
	// Upstream kinds sync right away (errors land in syncError, not here).
	if s.calendar != nil && (kind == calendar.KindCalDAV || ((kind == calendar.KindICS || kind == calendar.KindTrash) && row.URL != "")) {
		_ = s.calendar.Sync(ctx, created.ID)
		if fresh, gerr := s.store.GetCalendar(ctx, created.ID); gerr == nil {
			created = fresh
		}
	}
	s.calendarChanged()
	return CreateCalendar200JSONResponse(toAPICalendar(created)), nil
}

func (s *Server) DiscoverCalendars(ctx context.Context, req DiscoverCalendarsRequestObject) (DiscoverCalendarsResponseObject, error) {
	if httpctx.IdentityFrom(ctx) == nil {
		return nil, ErrUnauthenticated
	}
	username, password := "", ""
	if req.Body.Username != nil {
		username = *req.Body.Username
	}
	if req.Body.Password != nil {
		password = *req.Body.Password
	}
	cals, err := s.calendar.DiscoverCalendars(ctx, req.Body.Url, username, password)
	if err != nil {
		return DiscoverCalendars400JSONResponse{BadRequestJSONResponse(errBody("discover_failed", err.Error()))}, nil
	}
	out := make(DiscoverCalendars200JSONResponse, 0, len(cals))
	for _, c := range cals {
		item := struct {
			Description *string `json:"description,omitempty"`
			Name        string  `json:"name"`
			Path        string  `json:"path"`
		}{Name: c.Name, Path: c.Path}
		if c.Description != "" {
			d := c.Description
			item.Description = &d
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *Server) UpdateCalendar(ctx context.Context, req UpdateCalendarRequestObject) (UpdateCalendarResponseObject, error) {
	if httpctx.IdentityFrom(ctx) == nil {
		return nil, ErrUnauthenticated
	}
	var settings json.RawMessage
	if req.Body.Settings != nil {
		if raw, err := json.Marshal(req.Body.Settings); err == nil {
			settings = raw
		}
	} else if existing, err := s.store.GetCalendar(ctx, req.Id); err == nil {
		settings = existing.Settings
	}
	row, err := s.store.UpdateCalendar(ctx, req.Id, req.Body.Name, req.Body.Color, settings)
	if err != nil {
		return UpdateCalendar404JSONResponse{NotFoundJSONResponse(errBody("not_found", "no such calendar"))}, nil
	}
	s.calendarChanged()
	return UpdateCalendar200JSONResponse(toAPICalendar(row)), nil
}

func (s *Server) DeleteCalendar(ctx context.Context, req DeleteCalendarRequestObject) (DeleteCalendarResponseObject, error) {
	if httpctx.IdentityFrom(ctx) == nil {
		return nil, ErrUnauthenticated
	}
	gone, err := s.store.DeleteCalendar(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	if !gone {
		return DeleteCalendar404JSONResponse{NotFoundJSONResponse(errBody("not_found", "no such calendar"))}, nil
	}
	s.calendarChanged()
	return DeleteCalendar204Response{}, nil
}

func (s *Server) SyncCalendar(ctx context.Context, req SyncCalendarRequestObject) (SyncCalendarResponseObject, error) {
	if httpctx.IdentityFrom(ctx) == nil {
		return nil, ErrUnauthenticated
	}
	_ = s.calendar.Sync(ctx, req.Id) // errors land in syncError
	s.calendarChanged()
	row, err := s.store.GetCalendar(ctx, req.Id)
	if err != nil {
		return SyncCalendar404JSONResponse{NotFoundJSONResponse(errBody("not_found", "no such calendar"))}, nil
	}
	return SyncCalendar200JSONResponse(toAPICalendar(row)), nil
}

func (s *Server) ListCalendarEvents(ctx context.Context, req ListCalendarEventsRequestObject) (ListCalendarEventsResponseObject, error) {
	if httpctx.IdentityFrom(ctx) == nil {
		return nil, ErrUnauthenticated
	}
	from, to := req.Params.From, req.Params.To
	if !to.After(from) || to.Sub(from) > 400*24*time.Hour {
		to = from.Add(31 * 24 * time.Hour)
	}
	cals, err := s.store.ListCalendars(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]openapi_types.UUID, 0, len(cals))
	rules := map[openapi_types.UUID]string{}
	for _, c := range cals {
		ids = append(ids, c.ID)
	}
	if len(ids) == 0 {
		return ListCalendarEvents200JSONResponse{}, nil
	}
	rows, err := s.store.ListCalendarEventsInRange(ctx, ids, from, to)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		if r.RRule != "" && r.RecurrenceID == nil {
			rules[r.ID] = r.RRule
		}
	}
	instances := calendar.Expand(rows, from, to)
	out := make(ListCalendarEvents200JSONResponse, 0, len(instances))
	for _, inst := range instances {
		out = append(out, toAPIInstance(inst, rules[inst.EventID]))
	}
	return out, nil
}

func eventInputFromAPI(in CalendarEventInput) calendar.EventInput {
	out := calendar.EventInput{
		Title:    in.Title,
		StartsAt: in.StartsAt,
		EndsAt:   in.EndsAt,
	}
	if in.Location != nil {
		out.Location = *in.Location
	}
	if in.Description != nil {
		out.Description = *in.Description
	}
	if in.Url != nil {
		out.URL = *in.Url
	}
	if in.AllDay != nil {
		out.AllDay = *in.AllDay
	}
	if in.Rrule != nil {
		out.RRule = *in.Rrule
	}
	return out
}

func (s *Server) CreateCalendarEvent(ctx context.Context, req CreateCalendarEventRequestObject) (CreateCalendarEventResponseObject, error) {
	if httpctx.IdentityFrom(ctx) == nil {
		return nil, ErrUnauthenticated
	}
	row, err := s.calendar.CreateEvent(ctx, req.Body.CalendarId, eventInputFromAPI(req.Body.Event))
	if err != nil {
		return CreateCalendarEvent400JSONResponse{BadRequestJSONResponse(errBody("create_failed", err.Error()))}, nil
	}
	s.calendarChanged()
	inst := calendar.Instance{
		EventID: row.ID, CalendarID: row.CalendarID, UID: row.UID,
		Title: row.Title, Location: row.Location, Description: row.Description, URL: row.URL,
		Start: row.StartsAt, End: row.EndsAt, AllDay: row.AllDay,
		Recurring: row.RRule != "", OccurrenceStart: row.StartsAt,
	}
	return CreateCalendarEvent200JSONResponse(toAPIInstance(inst, row.RRule)), nil
}

func (s *Server) UpdateCalendarEvent(ctx context.Context, req UpdateCalendarEventRequestObject) (UpdateCalendarEventResponseObject, error) {
	if httpctx.IdentityFrom(ctx) == nil {
		return nil, ErrUnauthenticated
	}
	scope := calendar.ScopeAll
	if req.Body.Scope != nil {
		scope = calendar.EditScope(*req.Body.Scope)
	}
	occ := time.Time{}
	if req.Body.OccurrenceStart != nil {
		occ = *req.Body.OccurrenceStart
	}
	if err := s.calendar.UpdateEvent(ctx, req.Id, scope, occ, eventInputFromAPI(req.Body.Event)); err != nil {
		return UpdateCalendarEvent400JSONResponse{BadRequestJSONResponse(errBody("update_failed", err.Error()))}, nil
	}
	s.calendarChanged()
	return UpdateCalendarEvent204Response{}, nil
}

func (s *Server) DeleteCalendarEvent(ctx context.Context, req DeleteCalendarEventRequestObject) (DeleteCalendarEventResponseObject, error) {
	if httpctx.IdentityFrom(ctx) == nil {
		return nil, ErrUnauthenticated
	}
	scope := calendar.ScopeAll
	if req.Params.Scope != nil {
		scope = calendar.EditScope(*req.Params.Scope)
	}
	occ := time.Time{}
	if req.Params.OccurrenceStart != nil {
		occ = *req.Params.OccurrenceStart
	}
	if err := s.calendar.DeleteEvent(ctx, req.Id, scope, occ); err != nil {
		return DeleteCalendarEvent404JSONResponse{NotFoundJSONResponse(errBody("delete_failed", err.Error()))}, nil
	}
	s.calendarChanged()
	return DeleteCalendarEvent204Response{}, nil
}

// ---- subscription feeds -----------------------------------------------------
//
// A calendar published outward: one long random token in a URL that any
// iCalendar client can subscribe to — phones, Google Calendar, Home
// Assistant's remote calendar. The token is the whole credential, so the
// route is unauthenticated by design and lives outside /api.

// feedRefresh is the polling hint written into the feed. Clients treat it as
// a suggestion; most phones poll on their own schedule regardless.
const feedRefresh = 15 * time.Minute

func (s *Server) feedURL(ctx context.Context, token string) string {
	path := "/calendar/feed/" + token + ".ics"
	// The setup wizard defaults the app URL to whatever address the admin
	// reached it on — often 127.0.0.1 — and a phone can never use that. Then
	// the address this request came in on is the one subscribers can reach.
	if base := s.settings.AppURL(ctx); base != "" && !loopbackURL(base) {
		return base + path
	}
	if r := httpctx.Request(ctx); r != nil {
		return requestOrigin(r) + path
	}
	return path
}

// loopbackURL reports whether a base URL points at the machine itself.
func loopbackURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (s *Server) toAPICalendarFeed(ctx context.Context, feed *store.CalendarFeed) CalendarFeed {
	if feed == nil {
		return CalendarFeed{Published: false}
	}
	url := s.feedURL(ctx, feed.Token)
	created := feed.CreatedAt
	return CalendarFeed{Published: true, Url: &url, CreatedAt: &created}
}

func (s *Server) GetCalendarFeed(ctx context.Context, req GetCalendarFeedRequestObject) (GetCalendarFeedResponseObject, error) {
	if httpctx.IdentityFrom(ctx) == nil {
		return nil, ErrUnauthenticated
	}
	if _, err := s.store.GetCalendar(ctx, req.Id); err != nil {
		return GetCalendarFeed404JSONResponse{NotFoundJSONResponse(errBody("not_found", "no such calendar"))}, nil
	}
	feed, err := s.store.GetCalendarFeed(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return GetCalendarFeed200JSONResponse(s.toAPICalendarFeed(ctx, feed)), nil
}

func (s *Server) PublishCalendarFeed(ctx context.Context, req PublishCalendarFeedRequestObject) (PublishCalendarFeedResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if _, err := s.store.GetCalendar(ctx, req.Id); err != nil {
		return PublishCalendarFeed404JSONResponse{NotFoundJSONResponse(errBody("not_found", "no such calendar"))}, nil
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	feed, err := s.store.SetCalendarFeed(ctx, req.Id, base64.RawURLEncoding.EncodeToString(raw), id.User.ID)
	if err != nil {
		return nil, err
	}
	return PublishCalendarFeed200JSONResponse(s.toAPICalendarFeed(ctx, feed)), nil
}

func (s *Server) UnpublishCalendarFeed(ctx context.Context, req UnpublishCalendarFeedRequestObject) (UnpublishCalendarFeedResponseObject, error) {
	if httpctx.IdentityFrom(ctx) == nil {
		return nil, ErrUnauthenticated
	}
	if _, err := s.store.GetCalendar(ctx, req.Id); err != nil {
		return UnpublishCalendarFeed404JSONResponse{NotFoundJSONResponse(errBody("not_found", "no such calendar"))}, nil
	}
	if _, err := s.store.DeleteCalendarFeed(ctx, req.Id); err != nil {
		return nil, err
	}
	return UnpublishCalendarFeed204Response{}, nil
}

// HandleCalendarFeed serves GET /calendar/feed/{token}.ics. No session: the
// token is the credential. A wrong token is a 404 like any other missing
// path, so the route does not confirm which tokens exist.
func (s *Server) HandleCalendarFeed(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSuffix(chi.URLParam(r, "token"), ".ics")
	if token == "" || len(token) > 128 {
		http.NotFound(w, r)
		return
	}
	ctx := r.Context()
	feed, err := s.store.GetCalendarFeedByToken(ctx, token)
	if err != nil || feed == nil {
		http.NotFound(w, r)
		return
	}
	cal, err := s.store.GetCalendar(ctx, feed.CalendarID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	rows, err := s.store.ListCalendarEvents(ctx, cal.ID)
	if err != nil {
		http.Error(w, "calendar unavailable", http.StatusInternalServerError)
		return
	}
	// Subscribers poll; an unchanged calendar answers 304 from the newest
	// change it holds, without re-encoding anything.
	newest := cal.UpdatedAt
	for _, row := range rows {
		if row.UpdatedAt.After(newest) {
			newest = row.UpdatedAt
		}
	}
	etag := fmt.Sprintf(`"%d-%d"`, newest.UnixNano(), len(rows))
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	body, err := calendar.BuildFeed(cal.Name, rows, feedRefresh)
	if err != nil {
		http.Error(w, "calendar unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", `inline; filename="calendar.ics"`)
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Header().Set("ETag", etag)
	w.Header().Set("Last-Modified", newest.UTC().Format(http.TimeFormat))
	_, _ = w.Write(body)
}
