-- +goose Up

-- Assistant chat history, one rolling conversation per user. tool_trace
-- records which tools an assistant turn used and with what input — the
-- transparency record for "how did it know that".
create table assistant_messages (
    id         bigserial primary key,
    user_id    uuid not null references users (id) on delete cascade,
    role       text not null check (role in ('user', 'assistant')),
    content    text not null,
    tool_trace jsonb,
    created_at timestamptz not null default now()
);
create index assistant_messages_user_idx on assistant_messages (user_id, id);

-- +goose Down
drop table assistant_messages;
