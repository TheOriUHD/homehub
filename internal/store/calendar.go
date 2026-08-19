package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Calendar is one calendar: local, CalDAV (two-way), ICS feed, or a trash-
// collection schedule. CalDAV passwords live in the vault (calendar:<id>).
type Calendar struct {
	ID           uuid.UUID       `db:"id"`
	Name         string          `db:"name"`
	Color        string          `db:"color"`
	Kind         string          `db:"kind"`
	URL          string          `db:"url"`
	Username     string          `db:"username"`
	ReadOnly     bool            `db:"read_only"`
	Ctag         string          `db:"ctag"`
	LastSyncedAt *time.Time      `db:"last_synced_at"`
	SyncError    string          `db:"sync_error"`
	Settings     json.RawMessage `db:"settings"`
	CreatedAt    time.Time       `db:"created_at"`
	UpdatedAt    time.Time       `db:"updated_at"`
}

// CalendarEvent is a master event (possibly recurring, with EXDATEs) or an
// override instance (RecurrenceID set) of one.
type CalendarEvent struct {
	ID           uuid.UUID   `db:"id"`
	CalendarID   uuid.UUID   `db:"calendar_id"`
	UID          string      `db:"uid"`
	RecurrenceID *time.Time  `db:"recurrence_id"`
	Etag         string      `db:"etag"`
	Href         string      `db:"href"`
	Title        string      `db:"title"`
	Location     string      `db:"location"`
	Description  string      `db:"description"`
	URL          string      `db:"url"`
	StartsAt     time.Time   `db:"starts_at"`
	EndsAt       time.Time   `db:"ends_at"`
	AllDay       bool        `db:"all_day"`
	RRule        string      `db:"rrule"`
	Exdates      []time.Time `db:"exdates"`
	CreatedAt    time.Time   `db:"created_at"`
	UpdatedAt    time.Time   `db:"updated_at"`
}

const calendarCols = `id, name, color, kind, url, username, read_only, ctag,
	last_synced_at, sync_error, settings, created_at, updated_at`

const calEventCols = `id, calendar_id, uid, recurrence_id, etag, href, title, location,
	description, url, starts_at, ends_at, all_day, rrule, exdates, created_at, updated_at`

func (s *Store) ListCalendars(ctx context.Context) ([]Calendar, error) {
	return scanMany[Calendar](s.pool.Query(ctx,
		`select `+calendarCols+` from calendars order by created_at`))
}

func (s *Store) GetCalendar(ctx context.Context, id uuid.UUID) (*Calendar, error) {
	return scanOne[Calendar](s.pool.Query(ctx,
		`select `+calendarCols+` from calendars where id = $1`, id))
}

func (s *Store) CreateCalendar(ctx context.Context, c Calendar) (*Calendar, error) {
	return scanOne[Calendar](s.pool.Query(ctx, `
		insert into calendars (name, color, kind, url, username, read_only, settings)
		values ($1,$2,$3,$4,$5,$6,$7) returning `+calendarCols,
		c.Name, c.Color, c.Kind, c.URL, c.Username, c.ReadOnly, c.Settings))
}

func (s *Store) UpdateCalendar(ctx context.Context, id uuid.UUID, name, color string, settings json.RawMessage) (*Calendar, error) {
	return scanOne[Calendar](s.pool.Query(ctx, `
		update calendars set name=$2, color=$3, settings=$4, updated_at=now()
		where id=$1 returning `+calendarCols, id, name, color, settings))
}

func (s *Store) SetCalendarSyncState(ctx context.Context, id uuid.UUID, ctag, syncErr string) error {
	_, err := s.pool.Exec(ctx, `
		update calendars set ctag=$2, sync_error=$3, last_synced_at=now(), updated_at=now()
		where id=$1`, id, ctag, syncErr)
	return err
}

func (s *Store) DeleteCalendar(ctx context.Context, id uuid.UUID) (bool, error) {
	tag, err := s.pool.Exec(ctx, `delete from calendars where id=$1`, id)
	return tag.RowsAffected() > 0, err
}

// ---- events ----------------------------------------------------------------

// ListCalendarEventsInRange returns rows that could produce an instance in
// [from, to): non-recurring rows overlapping the window plus every recurring
// master (expansion decides which fire). Override rows come along with their
// masters via the recurring branch OR their own window overlap.
func (s *Store) ListCalendarEventsInRange(ctx context.Context, calendarIDs []uuid.UUID, from, to time.Time) ([]CalendarEvent, error) {
	return scanMany[CalendarEvent](s.pool.Query(ctx, `
		select `+calEventCols+` from calendar_events
		where calendar_id = any($1)
		  and (rrule <> '' or recurrence_id is not null
		       or (starts_at < $3 and ends_at > $2))
		order by starts_at`, calendarIDs, from, to))
}

func (s *Store) GetCalendarEvent(ctx context.Context, id uuid.UUID) (*CalendarEvent, error) {
	return scanOne[CalendarEvent](s.pool.Query(ctx,
		`select `+calEventCols+` from calendar_events where id=$1`, id))
}

// GetCalendarEventByUID fetches the master row (no recurrence id) for a UID.
func (s *Store) GetCalendarEventByUID(ctx context.Context, calendarID uuid.UUID, uid string) (*CalendarEvent, error) {
	return scanOne[CalendarEvent](s.pool.Query(ctx,
		`select `+calEventCols+` from calendar_events
		 where calendar_id=$1 and uid=$2 and recurrence_id is null`, calendarID, uid))
}

func (s *Store) CreateCalendarEvent(ctx context.Context, e CalendarEvent) (*CalendarEvent, error) {
	return scanOne[CalendarEvent](s.pool.Query(ctx, `
		insert into calendar_events (calendar_id, uid, recurrence_id, etag, href, title,
			location, description, url, starts_at, ends_at, all_day, rrule, exdates)
		values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		returning `+calEventCols,
		e.CalendarID, e.UID, e.RecurrenceID, e.Etag, e.Href, e.Title,
		e.Location, e.Description, e.URL, e.StartsAt, e.EndsAt, e.AllDay, e.RRule, e.Exdates))
}

func (s *Store) UpdateCalendarEvent(ctx context.Context, e CalendarEvent) (*CalendarEvent, error) {
	return scanOne[CalendarEvent](s.pool.Query(ctx, `
		update calendar_events set title=$2, location=$3, description=$4, url=$5,
			starts_at=$6, ends_at=$7, all_day=$8, rrule=$9, exdates=$10, etag=$11, updated_at=now()
		where id=$1 returning `+calEventCols,
		e.ID, e.Title, e.Location, e.Description, e.URL,
		e.StartsAt, e.EndsAt, e.AllDay, e.RRule, e.Exdates, e.Etag))
}

func (s *Store) DeleteCalendarEvent(ctx context.Context, id uuid.UUID) (bool, error) {
	tag, err := s.pool.Exec(ctx, `delete from calendar_events where id=$1`, id)
	return tag.RowsAffected() > 0, err
}

// DeleteCalendarEventsByUID removes a master and all its overrides.
func (s *Store) DeleteCalendarEventsByUID(ctx context.Context, calendarID uuid.UUID, uid string) error {
	_, err := s.pool.Exec(ctx,
		`delete from calendar_events where calendar_id=$1 and uid=$2`, calendarID, uid)
	return err
}

// ReplaceCalendarEvents swaps a feed calendar's entire event set (ICS/trash
// re-sync) atomically.
func (s *Store) ReplaceCalendarEvents(ctx context.Context, calendarID uuid.UUID, events []CalendarEvent) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `delete from calendar_events where calendar_id=$1`, calendarID); err != nil {
		return err
	}
	for _, e := range events {
		if _, err := tx.Exec(ctx, `
			insert into calendar_events (calendar_id, uid, recurrence_id, etag, href, title,
				location, description, url, starts_at, ends_at, all_day, rrule, exdates)
			values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
			on conflict do nothing`,
			calendarID, e.UID, e.RecurrenceID, e.Etag, e.Href, e.Title,
			e.Location, e.Description, e.URL, e.StartsAt, e.EndsAt, e.AllDay, e.RRule, e.Exdates); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// ListCalendarEventHrefs maps href → (id, etag) for one CalDAV calendar's
// sync diff.
func (s *Store) ListCalendarEventHrefs(ctx context.Context, calendarID uuid.UUID) (map[string]struct {
	ID   uuid.UUID
	Etag string
}, error) {
	rows, err := s.pool.Query(ctx,
		`select distinct on (href) href, id, etag from calendar_events
		 where calendar_id=$1 and href <> '' order by href, recurrence_id nulls first`, calendarID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]struct {
		ID   uuid.UUID
		Etag string
	}{}
	for rows.Next() {
		var href, etag string
		var id uuid.UUID
		if err := rows.Scan(&href, &id, &etag); err != nil {
			return nil, err
		}
		out[href] = struct {
			ID   uuid.UUID
			Etag string
		}{id, etag}
	}
	return out, rows.Err()
}

// DeleteCalendarEventsByHref removes every row (master + overrides) stored
// under one CalDAV object href.
func (s *Store) DeleteCalendarEventsByHref(ctx context.Context, calendarID uuid.UUID, href string) error {
	_, err := s.pool.Exec(ctx,
		`delete from calendar_events where calendar_id=$1 and href=$2`, calendarID, href)
	return err
}
