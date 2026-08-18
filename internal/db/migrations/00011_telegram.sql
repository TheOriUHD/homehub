-- +goose Up
-- Telegram bridge: chats that have been linked to a HomeHub user. A linked
-- chat talks to the assistant under that user's own permissions; everything
-- else the bot receives is politely refused.
CREATE TABLE telegram_chats (
    chat_id    BIGINT PRIMARY KEY,
    user_id    UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    label      TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE telegram_chats;
