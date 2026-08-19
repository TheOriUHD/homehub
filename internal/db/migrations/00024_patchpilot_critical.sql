-- +goose Up
-- Mark a managed host as critical / the hypervisor. Purely advisory: it does
-- not change what runs, but the UI escalates confirmation before any immediate
-- run that includes such a host (updates could reboot the hypervisor and
-- disrupt every service it hosts).
ALTER TABLE patchpilot_hosts ADD COLUMN critical BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE patchpilot_hosts DROP COLUMN critical;
