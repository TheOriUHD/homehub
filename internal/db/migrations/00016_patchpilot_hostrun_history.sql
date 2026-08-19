-- +goose Up

-- Preserve run history when a host is removed from a fleet. host_runs already
-- denormalize host_name, so a removed host's past runs stay fully readable;
-- only the live foreign key needs to go null instead of cascading the rows
-- away (which silently erased audit history on host deletion).
alter table patchpilot_host_runs alter column host_id drop not null;
alter table patchpilot_host_runs drop constraint patchpilot_host_runs_host_id_fkey;
alter table patchpilot_host_runs
    add constraint patchpilot_host_runs_host_id_fkey
    foreign key (host_id) references patchpilot_hosts (id) on delete set null;

-- +goose Down
alter table patchpilot_host_runs drop constraint patchpilot_host_runs_host_id_fkey;
alter table patchpilot_host_runs
    add constraint patchpilot_host_runs_host_id_fkey
    foreign key (host_id) references patchpilot_hosts (id) on delete cascade;
