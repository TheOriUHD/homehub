-- +goose Up
-- What the agent has proposed and is waiting to be told to run, on a surface
-- with no buttons: a Telegram chat answers /yes or /no. The web keeps its
-- pending actions in the page and posts them back, so only chat-like channels
-- land here.
--
-- One live proposal per user and channel: a newer proposal supersedes the old
-- one, an unanswered proposal expires rather than lingering as a live command,
-- and approving CLAIMS the row (delete … returning) so a double /yes cannot
-- run the same command twice.
create table assistant_pending_actions (
    user_id    uuid not null references users(id) on delete cascade,
    channel    text not null,
    parent_id  bigint not null references assistant_messages(id) on delete cascade,
    actions    jsonb not null,
    created_at timestamptz not null default now(),
    expires_at timestamptz not null,
    primary key (user_id, channel)
);

-- +goose Down
drop table assistant_pending_actions;
