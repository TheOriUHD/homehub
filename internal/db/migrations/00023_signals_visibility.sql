-- +goose Up
-- Coarse visibility control for the (deliberately global) signals stream:
-- an admin can hide the whole signals surface for a role or a single user,
-- rather than fragmenting the shared "one homelab, one truth" stream per RBAC
-- grant. A user's signals are hidden when their own flag is set, or when every
-- role they hold hides them (see Store.SignalsHiddenForUser).
ALTER TABLE roles ADD COLUMN hide_signals BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE users ADD COLUMN hide_signals BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE roles DROP COLUMN hide_signals;
ALTER TABLE users DROP COLUMN hide_signals;
