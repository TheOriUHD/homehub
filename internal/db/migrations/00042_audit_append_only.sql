-- +goose Up
-- The audit log is append-only: the application has no code path that edits
-- or deletes an entry, and this trigger makes the database refuse one too —
-- from a bug, a stray SQL console session on the HomeHub database, or an
-- injection — with the single exception of retention pruning, which marks its
-- own transaction with homehub.audit_retention = 'on' (store.PruneAudit).
--
-- This guards against the application and its database role, not against a
-- PostgreSQL superuser or the table owner, who can drop the trigger; nor does
-- it survive a backup restore of an older database, which replaces the table
-- wholesale (pg_restore --clean) along with everything else.
-- +goose StatementBegin
create or replace function audit_log_append_only() returns trigger
language plpgsql as $$
begin
    if tg_op = 'DELETE' and current_setting('homehub.audit_retention', true) = 'on' then
        return old;
    end if;
    raise exception 'audit_log is append-only: % refused', tg_op
        using errcode = 'insufficient_privilege';
end
$$;
-- +goose StatementEnd

create trigger audit_log_no_change
    before update or delete on audit_log
    for each row execute function audit_log_append_only();
create trigger audit_log_no_truncate
    before truncate on audit_log
    for each statement execute function audit_log_append_only();

-- +goose Down
drop trigger audit_log_no_truncate on audit_log;
drop trigger audit_log_no_change on audit_log;
drop function audit_log_append_only();
