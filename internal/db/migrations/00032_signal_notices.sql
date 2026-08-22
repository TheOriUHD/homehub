-- +goose Up
-- What has already been said about a condition, so the narrator can decide
-- whether saying it again is worth the interruption. A sensor that has been
-- low on battery for four days does not need a fifth message.
create table signal_notices (
    key            text primary key,
    last_sent_at   timestamptz not null,
    last_severity  text not null default '',
    sent_count     integer not null default 1
);

-- +goose Down
drop table signal_notices;
