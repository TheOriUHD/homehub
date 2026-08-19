-- +goose Up
-- Signals: the global notification stream. One row per observed condition,
-- deduplicated by key — a stateful condition (integration down, low battery,
-- storage full) updates its row while it persists, flips to 'recovered' when
-- it clears, and stays visible until someone dismisses it. One-shot events
-- (a failed update run, a completed backup worth knowing about) insert once
-- and wait for dismissal. Global, not per-user: one homelab, one truth.
CREATE TABLE signals (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    key          TEXT NOT NULL,               -- stable dedup key, namespaced per collector
    collector    TEXT NOT NULL,               -- which collector owns the key namespace
    source_type  TEXT NOT NULL DEFAULT '',    -- integration type id ("truenas", "patchpilot", …) or "system"
    source_id    UUID,                        -- integration instance, when applicable
    source_name  TEXT NOT NULL DEFAULT '',    -- display name of the source at observation time
    severity     TEXT NOT NULL,               -- info | warning | critical | recovered
    title        TEXT NOT NULL,
    body         TEXT NOT NULL DEFAULT '',
    detail       JSONB,                       -- deep payload: log excerpts, entity lists, run ids…
    auto_resolve BOOLEAN NOT NULL DEFAULT FALSE, -- stateful condition: clears itself when no longer observed
    count        INT  NOT NULL DEFAULT 1,     -- recurrences folded into this row
    first_seen   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen    TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at  TIMESTAMPTZ,                 -- set when the condition cleared (severity → recovered)
    dismissed_at TIMESTAMPTZ,
    dismissed_by UUID REFERENCES users (id) ON DELETE SET NULL
);

-- One live row per key: dismissal frees the key for a fresh occurrence.
CREATE UNIQUE INDEX signals_live_key ON signals (key) WHERE dismissed_at IS NULL;
CREATE INDEX signals_active_idx ON signals (dismissed_at, last_seen DESC);

-- +goose Down
DROP TABLE signals;
