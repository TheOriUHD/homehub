-- +goose Up
-- Central SSH host registry: one named entry per machine, reused everywhere
-- SSH is needed (Proxmox guest consoles, Minecraft networks, ad-hoc
-- terminals, the assistant). Secrets live in the vault under scope
-- "sshhost", keyed by the host id — never in this table.
create table ssh_hosts (
    id         uuid primary key default gen_random_uuid(),
    name       text not null unique,
    hostname   text not null,
    port       int  not null default 22,
    username   text not null,
    auth_type  text not null check (auth_type in ('password', 'key')),
    notes      text not null default '',
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

-- A per-resource credential may now simply POINT at a registry host instead
-- of carrying its own inline copy. Null host_id keeps the legacy inline
-- behaviour, so manual entry never stops working.
alter table ssh_credentials
    add column host_id uuid references ssh_hosts(id) on delete set null;

-- +goose Down
alter table ssh_credentials drop column if exists host_id;
drop table if exists ssh_hosts;
