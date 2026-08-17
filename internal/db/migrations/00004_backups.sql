-- +goose Up

-- Backup storage targets. Non-secret config in jsonb; credentials in the
-- vault under scope 'target:<id>'.
create table backup_targets (
    id             uuid primary key default gen_random_uuid(),
    type           text not null check (type in ('local', 'smb', 'nfs', 's3')),
    name           text not null,
    config         jsonb not null default '{}'::jsonb,
    retention_days int not null default 30 check (retention_days between 1 and 3650),
    encrypt        boolean not null default false,
    enabled        boolean not null default true,
    status         text not null default 'unknown' check (status in ('unknown', 'ok', 'error')),
    status_message text not null default '',
    created_at     timestamptz not null default now(),
    updated_at     timestamptz not null default now()
);
create unique index backup_targets_name_key on backup_targets (lower(name));

-- History of backup and restore runs.
create table backup_runs (
    id          bigint generated always as identity primary key,
    target_id   uuid references backup_targets (id) on delete set null,
    target_name text not null default '',
    kind        text not null check (kind in ('app-backup', 'restore', 'prune')),
    status      text not null default 'running' check (status in ('running', 'succeeded', 'failed')),
    file_path   text not null default '',
    size_bytes  bigint not null default 0,
    error       text not null default '',
    started_at  timestamptz not null default now(),
    finished_at timestamptz
);
create index backup_runs_history_idx on backup_runs (started_at desc);

-- +goose Down

drop table backup_runs;
drop table backup_targets;
