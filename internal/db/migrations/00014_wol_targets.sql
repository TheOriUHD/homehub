-- +goose Up
-- Wake-on-LAN targets: a per-user address book of machines to wake with one
-- click. Personal (scoped by user_id), like workspace files.
CREATE TABLE wol_targets (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    mac        TEXT NOT NULL,
    broadcast  TEXT NOT NULL DEFAULT '255.255.255.255',
    port       INTEGER NOT NULL DEFAULT 9,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX wol_targets_user_idx ON wol_targets (user_id, created_at);

-- +goose Down
DROP TABLE wol_targets;
