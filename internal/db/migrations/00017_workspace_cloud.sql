-- +goose Up
-- Workspace grows from a flat text store into a simple personal cloud:
-- folders, and files of any type. Text files keep their content in the
-- content column (editable, assistant-readable); binary files live on disk
-- under {DataDir}/workspace/{fileID} and the row carries metadata only.
ALTER TABLE user_files
    ADD COLUMN folder    TEXT    NOT NULL DEFAULT '',
    ADD COLUMN is_binary BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN mime      TEXT    NOT NULL DEFAULT '',
    ADD COLUMN blob_size BIGINT  NOT NULL DEFAULT 0;

-- Names are now unique per folder, not globally.
ALTER TABLE user_files DROP CONSTRAINT user_files_user_id_name_key;
ALTER TABLE user_files ADD CONSTRAINT user_files_user_folder_name_key UNIQUE (user_id, folder, name);

CREATE TABLE user_folders (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    path       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, path)
);

-- +goose Down
DROP TABLE user_folders;
ALTER TABLE user_files DROP CONSTRAINT user_files_user_folder_name_key;
ALTER TABLE user_files ADD CONSTRAINT user_files_user_id_name_key UNIQUE (user_id, name);
ALTER TABLE user_files
    DROP COLUMN folder,
    DROP COLUMN is_binary,
    DROP COLUMN mime,
    DROP COLUMN blob_size;
