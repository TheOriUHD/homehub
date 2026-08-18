-- +goose Up
-- The universal file converter's batches and per-file items. Strictly personal
-- data (no RBAC resource): every query is scoped by user_id, exactly like
-- user_files. On-disk working files live under {DataDir}/convert/{id}; only the
-- user-facing filenames live here (for Content-Disposition and zip entries).
-- Rows and their directories are reaped 24h after creation by a scheduled job.
CREATE TABLE conversions (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    status        TEXT NOT NULL DEFAULT 'queued', -- queued|running|done|partial|failed
    target_format TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at   TIMESTAMPTZ,
    expires_at    TIMESTAMPTZ NOT NULL
);
CREATE INDEX conversions_user_idx ON conversions (user_id, created_at DESC);
CREATE INDEX conversions_expires_idx ON conversions (expires_at);

CREATE TABLE conversion_items (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    conversion_id UUID NOT NULL REFERENCES conversions (id) ON DELETE CASCADE,
    position      INT NOT NULL DEFAULT 0,
    source_name   TEXT NOT NULL,
    source_bytes  BIGINT NOT NULL DEFAULT 0,
    source_format TEXT NOT NULL DEFAULT '',
    category      TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL DEFAULT 'pending', -- pending|converting|done|failed
    error         TEXT NOT NULL DEFAULT '',
    output_name   TEXT NOT NULL DEFAULT '',
    output_bytes  BIGINT NOT NULL DEFAULT 0,
    duration_ms   BIGINT NOT NULL DEFAULT 0
);
CREATE INDEX conversion_items_conversion_idx ON conversion_items (conversion_id, position);

-- +goose Down
DROP TABLE conversion_items;
DROP TABLE conversions;
