-- +goose Up

-- One-time 6-digit pairing codes. A code binds a companion plugin to an
-- integration instance; consumed on first successful pairing.
create table minecraft_pairings (
    code           text primary key,
    integration_id uuid not null references integrations (id) on delete cascade,
    created_at     timestamptz not null default now(),
    expires_at     timestamptz not null
);

create index minecraft_pairings_integration_idx on minecraft_pairings (integration_id);

-- Metadata reported by the paired companion; one row per integration.
create table minecraft_servers (
    integration_id    uuid primary key references integrations (id) on delete cascade,
    server_name       text not null default '',
    loader            text not null default '',
    mc_version        text not null default '',
    companion_version text not null default '',
    paired_at         timestamptz,
    last_connected_at timestamptz,
    last_remote_ip    text not null default ''
);

-- +goose Down
drop table minecraft_servers;
drop table minecraft_pairings;
