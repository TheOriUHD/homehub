-- +goose Up

-- Small per-user preference documents keyed by surface ("overview", …).
-- Presentation state that should follow the login across devices — not
-- settings (app-wide) and not notification routing (its own table).
create table user_prefs (
    user_id    uuid not null references users (id) on delete cascade,
    key        text not null,
    value      jsonb not null,
    updated_at timestamptz not null default now(),
    primary key (user_id, key)
);

-- +goose Down
drop table user_prefs;
