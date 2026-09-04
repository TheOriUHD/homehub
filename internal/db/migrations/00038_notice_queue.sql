-- +goose Up
-- Notices that wait for the digest instead of interrupting. One live row per
-- signal key: a repeat updates it, an all-clear marks it resolved ("came and
-- went") rather than adding a second line, and a fault returning after that
-- counts a bounce. Flushing stamps the rows with when and how they went out.
create table notice_queue (
    id          bigint generated always as identity primary key,
    key         text not null,
    signal_id   bigint,
    event       text not null,
    severity    text not null,
    title       text not null,
    body        text not null default '',
    source_name text not null default '',
    source_type text not null default '',
    detail      jsonb,
    bounces     integer not null default 0,
    queued_at   timestamptz not null default now(),
    updated_at  timestamptz not null default now(),
    resolved_at timestamptz,
    flushed_at  timestamptz,
    flush_kind  text not null default ''
);
create unique index notice_queue_live_key on notice_queue (key) where flushed_at is null;
create index notice_queue_unflushed on notice_queue (queued_at) where flushed_at is null;

-- +goose Down
drop table notice_queue;
