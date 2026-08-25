-- +goose Up
-- A calendar published outward as an iCalendar subscription. The token is
-- the whole credential — the URL is meant to be pasted into a phone or a
-- Home Assistant config, where nothing can log in — so it is long, random,
-- and rotated by replacing the row.
create table calendar_feeds (
    calendar_id uuid primary key references calendars (id) on delete cascade,
    token       text not null unique,
    created_by  uuid references users (id) on delete set null,
    created_at  timestamptz not null default now()
);

-- +goose Down
drop table calendar_feeds;
