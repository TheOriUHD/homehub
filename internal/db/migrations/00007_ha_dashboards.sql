-- +goose Up

-- Per-user Home Assistant dashboards: each user curates their own board per
-- instance. Layout is a validated JSON card list; empty layout = empty board.
create table ha_dashboards (
    user_id        uuid not null references users (id) on delete cascade,
    integration_id uuid not null references integrations (id) on delete cascade,
    layout         jsonb not null default '{"cards": []}',
    updated_at     timestamptz not null default now(),
    primary key (user_id, integration_id)
);

-- +goose Down
drop table ha_dashboards;
