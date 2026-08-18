-- +goose Up
-- Workspace files: the utilities area's per-user storage. Text-only (the
-- editor is a text editor), size-capped at the API layer. The assistant can
-- list and read the calling user's files as context.
CREATE TABLE user_files (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    content    TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, name)
);

CREATE INDEX user_files_user_idx ON user_files (user_id, updated_at DESC);

-- +goose Down
DROP TABLE user_files;
