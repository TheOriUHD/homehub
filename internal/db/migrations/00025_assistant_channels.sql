-- +goose Up
-- Assistant conversations are scoped per surface: the web UI and the Telegram
-- bridge each keep their own thread (never synced), and the web thread is
-- additionally scoped to a per-app-session conversation id so a fully closed
-- app starts fresh. Existing rows become the web channel's legacy history.
alter table assistant_messages
    add column channel text not null default 'web',
    add column conversation text not null default '';

create index assistant_messages_scope_idx
    on assistant_messages (user_id, channel, conversation, id desc);

-- +goose Down
drop index if exists assistant_messages_scope_idx;
alter table assistant_messages
    drop column if exists channel,
    drop column if exists conversation;
