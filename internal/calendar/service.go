package calendar

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/caldav"
	"github.com/google/uuid"

	"github.com/TheOriUHD/homehub/internal/store"
	"github.com/TheOriUHD/homehub/internal/vault"
)

// Kinds of calendars.
const (
	KindLocal  = "local"
	KindCalDAV = "caldav"
	KindICS    = "ics"
	KindTrash  = "trash"
)

// VaultScope stores CalDAV passwords per calendar.
func VaultScope(id uuid.UUID) string { return "calendar:" + id.String() }

const VaultKeyPassword = "password"

// Service owns calendars: CalDAV two-way sync, ICS/trash feed refresh, and
// local event CRUD with recurrence-scope editing.
type Service struct {
	store  *store.Store
	vault  *vault.Vault
	logger *slog.Logger
	client *http.Client
}

func NewService(st *store.Store, v *vault.Vault, logger *slog.Logger) *Service {
	return &Service{
		store:  st,
		vault:  v,
		logger: logger.With("component", "calendar"),
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// caldavClient builds an authenticated client for one calendar.
func (s *Service) caldavClient(ctx context.Context, cal *store.Calendar) (*caldav.Client, error) {
	password := ""
	if raw, err := s.vault.Open(ctx, VaultScope(cal.ID), VaultKeyPassword); err == nil {
		password = string(raw)
	}
	httpClient := webdav.HTTPClient(s.client)
	if cal.Username != "" {
		httpClient = webdav.HTTPClientWithBasicAuth(s.client, cal.Username, password)
	}
	return caldav.NewClient(httpClient, cal.URL)
}

// DiscoverCalendars validates CalDAV credentials and lists the collections
// behind an account URL — used by the "add calendar" flow.
func (s *Service) DiscoverCalendars(ctx context.Context, rawURL, username, password string) ([]caldav.Calendar, error) {
	httpClient := webdav.HTTPClient(s.client)
	if username != "" {
		httpClient = webdav.HTTPClientWithBasicAuth(s.client, username, password)
	}
	client, err := caldav.NewClient(httpClient, rawURL)
	if err != nil {
		return nil, err
	}
	principal, err := client.FindCurrentUserPrincipal(ctx)
	if err != nil {
		// Some servers point straight at the collection set; try as-is.
		principal = ""
	}
	homeSet, err := client.FindCalendarHomeSet(ctx, principal)
	if err != nil {
		homeSet = ""
	}
	cals, err := client.FindCalendars(ctx, homeSet)
	if err != nil {
		return nil, fmt.Errorf("could not list calendars: %w", err)
	}
	// Only event-capable collections.
	out := cals[:0]
	for _, c := range cals {
		ok := len(c.SupportedComponentSet) == 0
		for _, comp := range c.SupportedComponentSet {
			if comp == "VEVENT" {
				ok = true
			}
		}
		if ok {
			out = append(out, c)
		}
	}
	return out, nil
}

// Sync refreshes one calendar from its upstream (CalDAV diff or feed
// re-fetch). Local calendars are a no-op.
func (s *Service) Sync(ctx context.Context, id uuid.UUID) error {
	cal, err := s.store.GetCalendar(ctx, id)
	if err != nil {
		return err
	}
	var syncErr error
	switch cal.Kind {
	case KindCalDAV:
		syncErr = s.syncCalDAV(ctx, cal)
	case KindICS, KindTrash:
		syncErr = s.syncFeed(ctx, cal)
	default:
		return nil
	}
	msg := ""
	if syncErr != nil {
		msg = syncErr.Error()
		if len(msg) > 500 {
			msg = msg[:500]
		}
	}
	_ = s.store.SetCalendarSyncState(ctx, cal.ID, cal.Ctag, msg)
	return syncErr
}

// SyncAll refreshes every non-local calendar; wired to a periodic job.
func (s *Service) SyncAll(ctx context.Context) {
	cals, err := s.store.ListCalendars(ctx)
	if err != nil {
		return
	}
	for _, c := range cals {
		if c.Kind == KindLocal {
			continue
		}
		if err := s.Sync(ctx, c.ID); err != nil {
			s.logger.Warn("calendar sync failed", "calendar", c.Name, "error", err)
		}
	}
}

// syncFeed replaces an ICS/trash calendar's events from its URL.
func (s *Service) syncFeed(ctx context.Context, cal *store.Calendar) error {
	if cal.URL == "" {
		return nil // file-imported feed: events already stored
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cal.URL, nil)
	if err != nil {
		return err
	}
	res, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("feed returned %s", res.Status)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return err
	}
	events, err := ParseICS(cal.ID, data, "", "")
	if err != nil {
		return err
	}
	return s.store.ReplaceCalendarEvents(ctx, cal.ID, events)
}

// syncCalDAV diffs the collection by etag and pulls changed objects.
func (s *Service) syncCalDAV(ctx context.Context, cal *store.Calendar) error {
	client, err := s.caldavClient(ctx, cal)
	if err != nil {
		return err
	}
	// List every VEVENT object's href+etag.
	objs, err := client.QueryCalendar(ctx, cal.URL, &caldav.CalendarQuery{
		CompRequest: caldav.CalendarCompRequest{
			Name:  "VCALENDAR",
			Props: []string{"VERSION"},
			Comps: []caldav.CalendarCompRequest{{Name: "VEVENT", Props: []string{"UID"}}},
		},
		CompFilter: caldav.CompFilter{
			Name:  "VCALENDAR",
			Comps: []caldav.CompFilter{{Name: "VEVENT"}},
		},
	})
	if err != nil {
		return fmt.Errorf("calendar query: %w", err)
	}

	known, err := s.store.ListCalendarEventHrefs(ctx, cal.ID)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, obj := range objs {
		seen[obj.Path] = true
		if k, ok := known[obj.Path]; ok && k.Etag == obj.ETag && obj.ETag != "" {
			continue // unchanged
		}
		full, gerr := client.GetCalendarObject(ctx, obj.Path)
		if gerr != nil {
			s.logger.Warn("caldav fetch failed", "path", obj.Path, "error", gerr)
			continue
		}
		rows := eventsFromCal(cal.ID, full.Data, obj.Path, full.ETag)
		// Replace all rows stored under this href.
		if derr := s.store.DeleteCalendarEventsByHref(ctx, cal.ID, obj.Path); derr != nil {
			continue
		}
		for _, row := range rows {
			if _, cerr := s.store.CreateCalendarEvent(ctx, row); cerr != nil {
				s.logger.Warn("caldav store event", "path", obj.Path, "error", cerr)
			}
		}
	}
	// Objects gone upstream disappear here too.
	for href := range known {
		if !seen[href] {
			_ = s.store.DeleteCalendarEventsByHref(ctx, cal.ID, href)
		}
	}
	return nil
}

// ---- write-through ----------------------------------------------------------

// pushCalDAV writes a master (and its overrides) back to the server.
func (s *Service) pushCalDAV(ctx context.Context, cal *store.Calendar, master store.CalendarEvent, overrides []store.CalendarEvent) error {
	client, err := s.caldavClient(ctx, cal)
	if err != nil {
		return err
	}
	href := master.Href
	if href == "" {
		href = strings.TrimSuffix(cal.URL, "/") + "/" + master.UID + ".ics"
		if u := strings.SplitN(cal.URL, "://", 2); len(u) == 2 {
			// hrefs are path-absolute for caldav servers
			if i := strings.Index(u[1], "/"); i >= 0 {
				href = path.Join("/"+u[1][i+1:], master.UID+".ics")
			}
		}
	}
	data, err := BuildICS(master, overrides)
	if err != nil {
		return err
	}
	icsCal, err := parseForPut(data)
	if err != nil {
		return err
	}
	obj, err := client.PutCalendarObject(ctx, href, icsCal)
	if err != nil {
		return fmt.Errorf("caldav put: %w", err)
	}
	// Record href/etag on every row of the uid.
	newEtag := ""
	if obj != nil {
		newEtag = obj.ETag
	}
	_, _ = s.store.Pool().Exec(ctx,
		`update calendar_events set href=$3, etag=$4 where calendar_id=$1 and uid=$2`,
		cal.ID, master.UID, hrefOrPath(obj, href), newEtag)
	return nil
}

func hrefOrPath(obj *caldav.CalendarObject, fallback string) string {
	if obj != nil && obj.Path != "" {
		return obj.Path
	}
	return fallback
}

// deleteCalDAV removes the object behind a uid.
func (s *Service) deleteCalDAV(ctx context.Context, cal *store.Calendar, href string) error {
	if href == "" {
		return nil
	}
	client, err := s.caldavClient(ctx, cal)
	if err != nil {
		return err
	}
	return client.RemoveAll(ctx, href)
}

// ---- event editing with recurrence scopes ----------------------------------

// EditScope selects how far an edit to a recurring event reaches.
type EditScope string

const (
	ScopeAll       EditScope = "all"
	ScopeThis      EditScope = "this"
	ScopeFollowing EditScope = "following"
)

// EventInput is the editable surface of an event.
type EventInput struct {
	Title       string
	Location    string
	Description string
	URL         string
	StartsAt    time.Time
	EndsAt      time.Time
	AllDay      bool
	RRule       string
}

// CreateEvent adds a fresh event to a calendar (write-through for CalDAV).
func (s *Service) CreateEvent(ctx context.Context, calendarID uuid.UUID, in EventInput) (*store.CalendarEvent, error) {
	cal, err := s.store.GetCalendar(ctx, calendarID)
	if err != nil {
		return nil, err
	}
	if cal.ReadOnly || cal.Kind == KindICS || cal.Kind == KindTrash {
		return nil, fmt.Errorf("this calendar is read-only")
	}
	row := store.CalendarEvent{
		CalendarID:  calendarID,
		UID:         uuid.NewString() + "@homehub",
		Title:       in.Title,
		Location:    in.Location,
		Description: in.Description,
		URL:         in.URL,
		StartsAt:    in.StartsAt.UTC(),
		EndsAt:      in.EndsAt.UTC(),
		AllDay:      in.AllDay,
		RRule:       strings.TrimPrefix(in.RRule, "RRULE:"),
	}
	created, err := s.store.CreateCalendarEvent(ctx, row)
	if err != nil {
		return nil, err
	}
	if cal.Kind == KindCalDAV {
		if perr := s.pushCalDAV(ctx, cal, *created, nil); perr != nil {
			// Keep the local copy; sync surfaces the divergence.
			s.logger.Warn("caldav push (create)", "error", perr)
			return created, fmt.Errorf("saved locally, but the server rejected it: %w", perr)
		}
	}
	return created, nil
}

// UpdateEvent applies an edit at the requested scope. occurrence identifies
// the clicked instance for this/following scopes.
func (s *Service) UpdateEvent(ctx context.Context, eventID uuid.UUID, scope EditScope, occurrence time.Time, in EventInput) error {
	row, err := s.store.GetCalendarEvent(ctx, eventID)
	if err != nil {
		return err
	}
	cal, err := s.store.GetCalendar(ctx, row.CalendarID)
	if err != nil {
		return err
	}
	if cal.ReadOnly || cal.Kind == KindICS || cal.Kind == KindTrash {
		return fmt.Errorf("this calendar is read-only")
	}

	apply := func(e *store.CalendarEvent) {
		e.Title, e.Location, e.Description, e.URL = in.Title, in.Location, in.Description, in.URL
		e.StartsAt, e.EndsAt, e.AllDay = in.StartsAt.UTC(), in.EndsAt.UTC(), in.AllDay
	}

	// Editing an override row directly, or a non-recurring event, or scope
	// "all": straight update.
	if row.RecurrenceID != nil || row.RRule == "" || scope == ScopeAll || scope == "" {
		apply(row)
		if row.RecurrenceID == nil {
			row.RRule = strings.TrimPrefix(in.RRule, "RRULE:")
		}
		if _, err := s.store.UpdateCalendarEvent(ctx, *row); err != nil {
			return err
		}
		return s.pushUID(ctx, cal, row.CalendarID, row.UID)
	}

	switch scope {
	case ScopeThis:
		// Master keeps the slot excluded; an override carries the edit.
		row.Exdates = append(row.Exdates, occurrence.UTC())
		if _, err := s.store.UpdateCalendarEvent(ctx, *row); err != nil {
			return err
		}
		occ := occurrence.UTC()
		ov := store.CalendarEvent{
			CalendarID:   row.CalendarID,
			UID:          row.UID,
			RecurrenceID: &occ,
			Href:         row.Href,
			Etag:         row.Etag,
		}
		apply(&ov)
		if _, err := s.store.CreateCalendarEvent(ctx, ov); err != nil {
			return err
		}
		return s.pushUID(ctx, cal, row.CalendarID, row.UID)

	case ScopeFollowing:
		// Truncate the master before the occurrence, then start a new series.
		until := occurrence.UTC().Add(-time.Second)
		row.RRule = setRRuleUntil(row.RRule, until)
		if _, err := s.store.UpdateCalendarEvent(ctx, *row); err != nil {
			return err
		}
		if err := s.pushUID(ctx, cal, row.CalendarID, row.UID); err != nil {
			return err
		}
		_, cerr := s.CreateEvent(ctx, row.CalendarID, in)
		return cerr
	}
	return fmt.Errorf("unknown scope %q", scope)
}

// DeleteEvent removes at the requested scope.
func (s *Service) DeleteEvent(ctx context.Context, eventID uuid.UUID, scope EditScope, occurrence time.Time) error {
	row, err := s.store.GetCalendarEvent(ctx, eventID)
	if err != nil {
		return err
	}
	cal, err := s.store.GetCalendar(ctx, row.CalendarID)
	if err != nil {
		return err
	}
	if cal.ReadOnly || cal.Kind == KindICS || cal.Kind == KindTrash {
		return fmt.Errorf("this calendar is read-only")
	}

	// Deleting an override: restore nothing, just drop the row (the master
	// EXDATE stays — the slot remains vacant).
	if row.RecurrenceID != nil {
		if _, err := s.store.DeleteCalendarEvent(ctx, row.ID); err != nil {
			return err
		}
		return s.pushUID(ctx, cal, row.CalendarID, row.UID)
	}

	if row.RRule == "" || scope == ScopeAll || scope == "" {
		href := row.Href
		if err := s.store.DeleteCalendarEventsByUID(ctx, row.CalendarID, row.UID); err != nil {
			return err
		}
		if cal.Kind == KindCalDAV {
			if derr := s.deleteCalDAV(ctx, cal, href); derr != nil {
				s.logger.Warn("caldav delete", "error", derr)
			}
		}
		return nil
	}

	switch scope {
	case ScopeThis:
		row.Exdates = append(row.Exdates, occurrence.UTC())
		if _, err := s.store.UpdateCalendarEvent(ctx, *row); err != nil {
			return err
		}
		return s.pushUID(ctx, cal, row.CalendarID, row.UID)
	case ScopeFollowing:
		row.RRule = setRRuleUntil(row.RRule, occurrence.UTC().Add(-time.Second))
		if _, err := s.store.UpdateCalendarEvent(ctx, *row); err != nil {
			return err
		}
		return s.pushUID(ctx, cal, row.CalendarID, row.UID)
	}
	return fmt.Errorf("unknown scope %q", scope)
}

// pushUID write-throughs the current state of one uid to CalDAV.
func (s *Service) pushUID(ctx context.Context, cal *store.Calendar, calendarID uuid.UUID, uid string) error {
	if cal.Kind != KindCalDAV {
		return nil
	}
	master, err := s.store.GetCalendarEventByUID(ctx, calendarID, uid)
	if err != nil {
		return nil // master gone; nothing to push
	}
	rows, err := s.store.ListCalendarEventsInRange(ctx, []uuid.UUID{calendarID},
		time.Now().AddDate(-5, 0, 0), time.Now().AddDate(5, 0, 0))
	if err != nil {
		return err
	}
	var overrides []store.CalendarEvent
	for _, r := range rows {
		if r.UID == uid && r.RecurrenceID != nil {
			overrides = append(overrides, r)
		}
	}
	if perr := s.pushCalDAV(ctx, cal, *master, overrides); perr != nil {
		s.logger.Warn("caldav push", "uid", uid, "error", perr)
		return fmt.Errorf("saved locally, but the server rejected it: %w", perr)
	}
	return nil
}

// setRRuleUntil rewrites an RRULE's UNTIL (dropping COUNT).
func setRRuleUntil(rule string, until time.Time) string {
	parts := strings.Split(rule, ";")
	out := parts[:0]
	for _, p := range parts {
		up := strings.ToUpper(p)
		if strings.HasPrefix(up, "UNTIL=") || strings.HasPrefix(up, "COUNT=") {
			continue
		}
		if p != "" {
			out = append(out, p)
		}
	}
	out = append(out, "UNTIL="+until.UTC().Format("20060102T150405Z"))
	return strings.Join(out, ";")
}

// TrashSettings configures a trash-collection calendar's reminders.
type TrashSettings struct {
	// LeadHours: how far ahead of each pickup to raise a signal (several
	// stages, e.g. [72, 24]).
	LeadHours []int `json:"leadHours"`
}

// ParseTrashSettings reads a calendar's trash settings with defaults.
func ParseTrashSettings(raw json.RawMessage) TrashSettings {
	out := TrashSettings{LeadHours: []int{24}}
	if len(raw) > 0 {
		var ts TrashSettings
		if err := json.Unmarshal(raw, &ts); err == nil && len(ts.LeadHours) > 0 {
			out = ts
		}
	}
	return out
}
