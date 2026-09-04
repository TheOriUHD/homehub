-- +goose Up
-- The notice gate remembers the last *fault* it reported separately from the
-- last message of any kind: after an all-clear went out, the fault's return
-- must be judged against the fault that was reported, not against
-- "recovered" — which ranks below everything and made every return look
-- like an escalation worth an interruption. Flaps counts fault→clear→fault
-- cycles inside a window, so a bouncing condition stops interrupting.
alter table signal_notices
    add column last_fault_at        timestamptz,
    add column last_fault_severity  text not null default '',
    add column flaps                integer not null default 0,
    add column flap_window_start    timestamptz;

-- +goose Down
alter table signal_notices
    drop column flap_window_start,
    drop column flaps,
    drop column last_fault_severity,
    drop column last_fault_at;
