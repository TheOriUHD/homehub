-- +goose Up
-- A PatchPilot host may be bound to an entry in the central SSH registry. The
-- registry credential does the one-time bootstrap (and any re-run of it), and
-- the host's address follows the registry entry so an edit there applies on
-- the next connection. Runs themselves keep using the dedicated patchpilot
-- account and the fleet key. hostname/ssh_port stay as a snapshot for the
-- day the entry is deleted.
alter table patchpilot_hosts
    add column ssh_host_id uuid references ssh_hosts (id) on delete set null;
create index patchpilot_hosts_ssh_host_idx on patchpilot_hosts (ssh_host_id);

-- +goose Down
drop index patchpilot_hosts_ssh_host_idx;
alter table patchpilot_hosts drop column ssh_host_id;
