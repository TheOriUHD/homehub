-- +goose Up

-- Identity ------------------------------------------------------------------

create table users (
    id                   uuid primary key default gen_random_uuid(),
    username             text not null,
    display_name         text not null default '',
    email                text,
    password_hash        text,
    must_change_password boolean not null default false,
    status               text not null default 'active' check (status in ('active', 'disabled')),
    failed_login_count   int not null default 0,
    locked_until         timestamptz,
    password_changed_at  timestamptz,
    last_login_at        timestamptz,
    created_at           timestamptz not null default now(),
    updated_at           timestamptz not null default now()
);
create unique index users_username_key on users (lower(username));
create unique index users_email_key on users (lower(email)) where email is not null;

create table roles (
    id          uuid primary key default gen_random_uuid(),
    name        text not null,
    description text not null default '',
    is_system   boolean not null default false,
    created_at  timestamptz not null default now(),
    updated_at  timestamptz not null default now()
);
create unique index roles_name_key on roles (lower(name));

create table user_roles (
    user_id uuid not null references users (id) on delete cascade,
    role_id uuid not null references roles (id) on delete cascade,
    primary key (user_id, role_id)
);

-- A grant belongs to exactly one of: a role, or a user (per-user override).
-- selector is a resource path pattern: segments separated by '/', where '*'
-- matches one segment and '**' matches any subtree. levels is a subset of
-- {view, control, configure, delete}. Explicit deny beats any allow.
create table grants (
    id         uuid primary key default gen_random_uuid(),
    role_id    uuid references roles (id) on delete cascade,
    user_id    uuid references users (id) on delete cascade,
    selector   text not null,
    levels     text[] not null check (levels <> '{}' and levels <@ array['view', 'control', 'configure', 'delete']),
    effect     text not null default 'allow' check (effect in ('allow', 'deny')),
    created_at timestamptz not null default now(),
    check (num_nonnulls(role_id, user_id) = 1)
);
create index grants_role_idx on grants (role_id) where role_id is not null;
create index grants_user_idx on grants (user_id) where user_id is not null;

create table sessions (
    id              uuid primary key default gen_random_uuid(),
    user_id         uuid not null references users (id) on delete cascade,
    token_hash      bytea not null unique,
    created_at      timestamptz not null default now(),
    last_seen_at    timestamptz not null default now(),
    expires_at      timestamptz not null,
    ip              text not null default '',
    user_agent      text not null default '',
    revoked_at      timestamptz,
    view_as_role_id uuid references roles (id) on delete set null
);
create index sessions_user_idx on sessions (user_id);
create index sessions_expiry_idx on sessions (expires_at);

-- MFA -----------------------------------------------------------------------

create table totp_credentials (
    user_id           uuid primary key references users (id) on delete cascade,
    secret_ciphertext bytea not null,
    confirmed_at      timestamptz,
    created_at        timestamptz not null default now()
);

create table recovery_codes (
    id         uuid primary key default gen_random_uuid(),
    user_id    uuid not null references users (id) on delete cascade,
    code_hash  bytea not null,
    used_at    timestamptz,
    created_at timestamptz not null default now()
);
create index recovery_codes_user_idx on recovery_codes (user_id);

create table webauthn_credentials (
    id               uuid primary key default gen_random_uuid(),
    user_id          uuid not null references users (id) on delete cascade,
    name             text not null default '',
    credential_id    bytea not null unique,
    public_key       bytea not null,
    attestation_type text not null default '',
    transports       text[] not null default '{}',
    aaguid           bytea,
    sign_count       bigint not null default 0,
    backup_eligible  boolean not null default false,
    backup_state     boolean not null default false,
    created_at       timestamptz not null default now(),
    last_used_at     timestamptz
);
create index webauthn_credentials_user_idx on webauthn_credentials (user_id);

-- Admin-created account onboarding: either an invite link (token_hash set) or
-- a one-time temporary password (stored on the user row, must_change_password).
create table invites (
    id         uuid primary key default gen_random_uuid(),
    user_id    uuid not null references users (id) on delete cascade,
    token_hash bytea unique,
    created_by uuid references users (id) on delete set null,
    expires_at timestamptz not null,
    used_at    timestamptz,
    created_at timestamptz not null default now()
);

-- Platform ------------------------------------------------------------------

-- Envelope-encrypted secret store. The wrapping key (KEK) lives outside the
-- database (env var or keyfile in the data dir) and is never persisted here.
create table secrets (
    id          uuid primary key default gen_random_uuid(),
    scope       text not null,
    name        text not null,
    key_version int not null,
    dek_wrapped bytea not null,
    nonce       bytea not null,
    ciphertext  bytea not null,
    created_at  timestamptz not null default now(),
    updated_at  timestamptz not null default now(),
    unique (scope, name)
);

create table settings (
    key        text primary key,
    value      jsonb not null,
    updated_at timestamptz not null default now()
);

create table audit_log (
    id         bigint generated always as identity primary key,
    ts         timestamptz not null default now(),
    actor_kind text not null check (actor_kind in ('user', 'system', 'agent')),
    actor_id   uuid,
    actor_name text not null default '',
    session_id uuid,
    action     text not null,
    resource   text not null default '',
    outcome    text not null check (outcome in ('success', 'failure', 'denied')),
    ip         text not null default '',
    detail     jsonb not null default '{}'::jsonb
);
create index audit_log_ts_idx on audit_log (ts desc);
create index audit_log_actor_idx on audit_log (actor_id, ts desc);
create index audit_log_action_idx on audit_log (action, ts desc);
create index audit_log_resource_idx on audit_log (resource, ts desc);

create table jobs (
    id          bigint generated always as identity primary key,
    kind        text not null,
    payload     jsonb not null default '{}'::jsonb,
    status      text not null default 'pending' check (status in ('pending', 'running', 'succeeded', 'failed', 'cancelled')),
    priority    int not null default 0,
    run_at      timestamptz not null default now(),
    started_at  timestamptz,
    finished_at timestamptz,
    attempts    int not null default 0,
    max_attempts int not null default 3,
    last_error  text not null default '',
    created_at  timestamptz not null default now()
);
create index jobs_claim_idx on jobs (run_at, priority desc) where status = 'pending';
create index jobs_history_idx on jobs (kind, created_at desc);

create table schedules (
    id          uuid primary key default gen_random_uuid(),
    name        text not null,
    kind        text not null,
    payload     jsonb not null default '{}'::jsonb,
    cron_expr   text not null,
    enabled     boolean not null default true,
    last_run_at timestamptz,
    next_run_at timestamptz,
    created_at  timestamptz not null default now(),
    updated_at  timestamptz not null default now()
);
create index schedules_due_idx on schedules (next_run_at) where enabled;

create table notifications (
    id         bigint generated always as identity primary key,
    user_id    uuid not null references users (id) on delete cascade,
    ts         timestamptz not null default now(),
    severity   text not null default 'info' check (severity in ('info', 'success', 'warning', 'error')),
    title      text not null,
    body       text not null default '',
    resource   text not null default '',
    event_type text not null default '',
    read_at    timestamptz
);
create index notifications_user_idx on notifications (user_id, ts desc);
create index notifications_unread_idx on notifications (user_id) where read_at is null;

-- +goose Down

drop table notifications;
drop table schedules;
drop table jobs;
drop table audit_log;
drop table settings;
drop table secrets;
drop table invites;
drop table webauthn_credentials;
drop table recovery_codes;
drop table totp_credentials;
drop table sessions;
drop table grants;
drop table user_roles;
drop table roles;
drop table users;
