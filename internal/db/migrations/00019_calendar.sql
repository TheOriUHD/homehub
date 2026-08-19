-- +goose Up
-- Calendars: local ones live here outright; CalDAV calendars two-way sync
-- (credentials in the vault, scope calendar:<id>); ICS subscriptions and
-- trash-collection (Müllabfuhr) schedules are read-only feeds. Events store
-- masters with RRULE + EXDATEs plus override rows (recurrence_id) for
-- modified single occurrences — expansion happens at query time.
CREATE TABLE calendars (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name           TEXT NOT NULL,
    color          TEXT NOT NULL DEFAULT '#4f46e5',
    kind           TEXT NOT NULL,              -- local | caldav | ics | trash
    url            TEXT NOT NULL DEFAULT '',   -- caldav collection / ics feed
    username       TEXT NOT NULL DEFAULT '',
    read_only      BOOLEAN NOT NULL DEFAULT FALSE,
    ctag           TEXT NOT NULL DEFAULT '',   -- caldav collection tag
    last_synced_at TIMESTAMPTZ,
    sync_error     TEXT NOT NULL DEFAULT '',
    settings       JSONB,                      -- trash reminders, feed options…
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE calendar_events (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    calendar_id   UUID NOT NULL REFERENCES calendars (id) ON DELETE CASCADE,
    uid           TEXT NOT NULL,              -- iCalendar UID
    recurrence_id TIMESTAMPTZ,                -- set on override instances
    etag          TEXT NOT NULL DEFAULT '',   -- caldav object etag
    href          TEXT NOT NULL DEFAULT '',   -- caldav object href
    title         TEXT NOT NULL DEFAULT '',
    location      TEXT NOT NULL DEFAULT '',
    description   TEXT NOT NULL DEFAULT '',
    url           TEXT NOT NULL DEFAULT '',
    starts_at     TIMESTAMPTZ NOT NULL,
    ends_at       TIMESTAMPTZ NOT NULL,
    all_day       BOOLEAN NOT NULL DEFAULT FALSE,
    rrule         TEXT NOT NULL DEFAULT '',   -- "FREQ=WEEKLY;BYDAY=MO" (no RRULE: prefix)
    exdates       TIMESTAMPTZ[] NOT NULL DEFAULT '{}',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX calendar_events_uid_key
    ON calendar_events (calendar_id, uid, COALESCE(recurrence_id, 'epoch'::timestamptz));
CREATE INDEX calendar_events_range_idx ON calendar_events (calendar_id, starts_at);

-- +goose Down
DROP TABLE calendar_events;
DROP TABLE calendars;
