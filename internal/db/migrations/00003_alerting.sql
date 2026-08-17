-- +goose Up

-- Threshold alert rules over the metric history. State is edge-triggered:
-- transitions ok→firing and firing→ok emit notifications.
create table alert_rules (
    id                uuid primary key default gen_random_uuid(),
    name              text not null,
    resource          text not null,
    metric            text not null,
    operator          text not null check (operator in ('gt', 'lt')),
    threshold         double precision not null,
    sustained_seconds int not null default 60 check (sustained_seconds between 30 and 86400),
    severity          text not null default 'warning' check (severity in ('info', 'warning', 'error')),
    enabled           boolean not null default true,
    state             text not null default 'ok' check (state in ('ok', 'firing', 'unknown')),
    state_changed_at  timestamptz not null default now(),
    created_by        uuid references users (id) on delete set null,
    created_at        timestamptz not null default now(),
    updated_at        timestamptz not null default now()
);
create index alert_rules_enabled_idx on alert_rules (enabled) where enabled;

-- Outbound notification channels. Non-secret config in jsonb; secrets
-- (SMTP password, bot tokens, webhook auth values) in the vault under
-- scope 'channel:<id>'.
create table notification_channels (
    id         uuid primary key default gen_random_uuid(),
    type       text not null check (type in ('webhook', 'email', 'ntfy', 'gotify', 'telegram', 'discord')),
    name       text not null,
    config     jsonb not null default '{}'::jsonb,
    enabled    boolean not null default true,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);
create unique index notification_channels_name_key on notification_channels (lower(name));

-- Per-user, per-event-type routing. A row's absence means the default:
-- in-app only. channel_ids lists the outbound channels this user wants for
-- the event type.
create table notification_prefs (
    user_id     uuid not null references users (id) on delete cascade,
    event_type  text not null,
    inapp       boolean not null default true,
    channel_ids uuid[] not null default '{}',
    primary key (user_id, event_type)
);

-- +goose Down

drop table notification_prefs;
drop table notification_channels;
drop table alert_rules;
