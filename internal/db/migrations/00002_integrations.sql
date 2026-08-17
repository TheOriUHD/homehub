-- +goose Up

-- Integration instances. Non-secret configuration lives in config; secrets
-- (API tokens, passwords) live in the vault under scope 'integration:<id>'.
create table integrations (
    id                uuid primary key default gen_random_uuid(),
    type              text not null,
    name              text not null,
    config            jsonb not null default '{}'::jsonb,
    enabled           boolean not null default true,
    status            text not null default 'pending'
                      check (status in ('pending', 'connecting', 'connected', 'degraded', 'unreachable', 'disabled')),
    status_message    text not null default '',
    status_changed_at timestamptz not null default now(),
    created_at        timestamptz not null default now(),
    updated_at        timestamptz not null default now()
);
create unique index integrations_name_key on integrations (lower(name));
create index integrations_type_idx on integrations (type);

-- History engine v1: raw samples plus 5m/1h rollups with per-tier retention.
create table metric_series (
    id         bigint generated always as identity primary key,
    resource   text not null,
    metric     text not null,
    unit       text not null default '',
    created_at timestamptz not null default now(),
    unique (resource, metric)
);

create table metric_samples (
    series_id bigint not null references metric_series (id) on delete cascade,
    ts        timestamptz not null,
    value     double precision not null
);
create index metric_samples_series_ts_idx on metric_samples (series_id, ts desc);

create table metric_rollups (
    series_id    bigint not null references metric_series (id) on delete cascade,
    bucket       text not null check (bucket in ('5m', '1h')),
    ts           timestamptz not null,
    avg_value    double precision not null,
    min_value    double precision not null,
    max_value    double precision not null,
    sample_count int not null,
    primary key (series_id, bucket, ts)
);

-- Per-resource SSH access (e.g. a Proxmox guest). The password or private
-- key lives in the vault under scope 'ssh', name = resource path.
create table ssh_credentials (
    id         uuid primary key default gen_random_uuid(),
    resource   text not null unique,
    host       text not null default '',
    port       int not null default 22,
    username   text not null,
    auth_type  text not null check (auth_type in ('password', 'key')),
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

-- +goose Down

drop table ssh_credentials;
drop table metric_rollups;
drop table metric_samples;
drop table metric_series;
drop table integrations;
