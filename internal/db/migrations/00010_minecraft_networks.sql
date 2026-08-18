-- +goose Up

-- One Minecraft integration is now a NETWORK (a proxy plus its
-- subservers, or any grouping the operator likes); companions bind to a
-- server row within it. Existing single-server instances are wrapped in
-- place: the server row reuses the integration's id, which keeps every
-- issued agent token (mcs1.<uuid>.…) and vault slot valid.
create table minecraft_network_servers (
    id                uuid primary key default gen_random_uuid(),
    integration_id    uuid not null references integrations (id) on delete cascade,
    label             text not null default '',
    server_name       text not null default '',
    loader            text not null default '',
    mc_version        text not null default '',
    companion_version text not null default '',
    paired_at         timestamptz,
    last_connected_at timestamptz,
    last_remote_ip    text not null default '',
    created_at        timestamptz not null default now()
);
create index minecraft_network_servers_integration_idx on minecraft_network_servers (integration_id);

insert into minecraft_network_servers
    (id, integration_id, label, server_name, loader, mc_version, companion_version, paired_at, last_connected_at, last_remote_ip)
select integration_id, integration_id,
       coalesce(nullif(server_name, ''), 'Server'),
       server_name, loader, mc_version, companion_version, paired_at, last_connected_at, last_remote_ip
from minecraft_servers;

drop table minecraft_servers;

-- A pairing code now carries the label the new server will wear.
alter table minecraft_pairings add column label text not null default '';

-- +goose Down
alter table minecraft_pairings drop column label;
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
insert into minecraft_servers
select integration_id, server_name, loader, mc_version, companion_version, paired_at, last_connected_at, last_remote_ip
from minecraft_network_servers where id = integration_id;
drop table minecraft_network_servers;
