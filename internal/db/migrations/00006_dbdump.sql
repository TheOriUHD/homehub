-- +goose Up

-- Database dumps land in the same run history as app backups; the kind
-- constraint grows to admit them, and the dumped database is recorded.
alter table backup_runs drop constraint if exists backup_runs_kind_check;
alter table backup_runs add constraint backup_runs_kind_check
    check (kind in ('app-backup', 'restore', 'prune', 'db-dump'));
alter table backup_runs add column if not exists source text not null default '';

-- +goose Down
alter table backup_runs drop column if exists source;
alter table backup_runs drop constraint if exists backup_runs_kind_check;
alter table backup_runs add constraint backup_runs_kind_check
    check (kind in ('app-backup', 'restore', 'prune'));
