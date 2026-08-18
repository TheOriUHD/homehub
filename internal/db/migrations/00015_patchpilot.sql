-- +goose Up

-- PatchPilot: one integration row is a "fleet"; managed machines are host
-- rows within it. An agentless SSH update manager — HomeHub creates a
-- dedicated `patchpilot` account + least-privilege sudoers on each host and
-- drives the package manager over SSH. Nothing is installed on the target.

create table patchpilot_hosts (
    id              uuid primary key default gen_random_uuid(),
    integration_id  uuid not null references integrations (id) on delete cascade,
    name            text not null,
    hostname        text not null,
    ssh_port        int  not null default 22,
    ssh_user        text not null default 'patchpilot',
    host_type       text not null default 'standard',      -- standard
    package_manager text not null default '',              -- apt|apt_proxmox|dnf|pacman|zypper|apk
    os_pretty       text not null default '',
    arch            text not null default '',
    is_proxmox      boolean not null default false,
    enabled         boolean not null default true,
    auto_reboot     boolean not null default false,
    notes           text not null default '',
    -- last dry-run snapshot, so the host grid shows pending counts without a live probe
    updates_pending int  not null default 0,
    reboot_required boolean not null default false,
    last_checked_at timestamptz,
    last_run_status text not null default '',
    last_run_at     timestamptz,
    created_at      timestamptz not null default now(),
    updated_at      timestamptz not null default now(),
    unique (integration_id, name)
);
create index patchpilot_hosts_integration_idx on patchpilot_hosts (integration_id);

create table patchpilot_schedules (
    id              uuid primary key default gen_random_uuid(),
    integration_id  uuid not null references integrations (id) on delete cascade,
    name            text not null,
    cron_expr       text not null,                          -- standard 5-field
    enabled         boolean not null default true,
    suppress_reboot boolean not null default false,
    last_run_at     timestamptz,
    next_run_at     timestamptz,
    last_skipped_at timestamptz,
    last_skip_reason text not null default '',
    created_at      timestamptz not null default now(),
    updated_at      timestamptz not null default now(),
    unique (integration_id, name)
);
create index patchpilot_schedules_integration_idx on patchpilot_schedules (integration_id);
create index patchpilot_schedules_next_idx on patchpilot_schedules (enabled, next_run_at);

create table patchpilot_schedule_hosts (
    schedule_id uuid not null references patchpilot_schedules (id) on delete cascade,
    host_id     uuid not null references patchpilot_hosts (id) on delete cascade,
    order_index int  not null default 0,
    primary key (schedule_id, host_id)
);

create table patchpilot_runs (
    id             uuid primary key default gen_random_uuid(),
    integration_id uuid not null references integrations (id) on delete cascade,
    schedule_id    uuid references patchpilot_schedules (id) on delete set null,
    trigger        text not null,                           -- manual|scheduled
    status         text not null default 'running',         -- running|success|partial|failed|cancelled
    started_at     timestamptz not null default now(),
    finished_at    timestamptz,
    summary        text not null default ''
);
create index patchpilot_runs_integration_idx on patchpilot_runs (integration_id, started_at desc);

create table patchpilot_host_runs (
    id               uuid primary key default gen_random_uuid(),
    run_id           uuid not null references patchpilot_runs (id) on delete cascade,
    host_id          uuid not null references patchpilot_hosts (id) on delete cascade,
    host_name        text not null default '',
    status           text not null default 'pending',       -- pending|running|success|failed|skipped
    exit_code        int,
    packages_upgraded int not null default 0,
    reboot_required  boolean not null default false,
    error_summary    text not null default '',
    log              text not null default '',              -- captured output (ANSI-stripped)
    started_at       timestamptz,
    finished_at      timestamptz
);
create index patchpilot_host_runs_run_idx on patchpilot_host_runs (run_id);

-- +goose Down
drop table patchpilot_host_runs;
drop table patchpilot_runs;
drop table patchpilot_schedule_hosts;
drop table patchpilot_schedules;
drop table patchpilot_hosts;
