-- +goose Up
-- Every message that actually went out about a condition, with the state it
-- described. signal_notices knows THAT a condition was reported; this knows
-- what was said, so the assistant can answer "that printer alert" long after
-- the message has scrolled out of the conversation window.
create table signal_notice_log (
    id          bigint generated always as identity primary key,
    key         text not null,
    event       text not null,
    severity    text not null,
    title       text not null,
    source_name text not null default '',
    text        text not null,
    sent_at     timestamptz not null default now()
);
create index signal_notice_log_sent_idx on signal_notice_log (sent_at desc);

-- A recovery is not announced the moment a signal clears: it waits, and the
-- fault coming back inside the wait cancels it. The wait survives a restart
-- because restarts are exactly when a flapping fault looks healed.
alter table signal_notices add column recovery_due_at timestamptz;
alter table signal_notices add column recovery_signal_id bigint;

-- +goose Down
alter table signal_notices drop column if exists recovery_signal_id;
alter table signal_notices drop column if exists recovery_due_at;
drop table signal_notice_log;
