-- +goose Up
-- Pin each managed host's SSH host key (trust-on-first-use). Empty until the
-- next successful connection captures it; thereafter every connection is
-- verified against it, so a LAN man-in-the-middle can't silently impersonate
-- a host and capture credentials or fake a patch run.
ALTER TABLE patchpilot_hosts ADD COLUMN host_key TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE patchpilot_hosts DROP COLUMN host_key;
