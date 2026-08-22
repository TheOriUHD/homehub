-- +goose Up
-- Wall-clock time to produce an answer, across every model round and tool
-- call in that turn. Null on rows written before it was measured.
alter table assistant_messages add column duration_ms bigint;

-- +goose Down
alter table assistant_messages drop column if exists duration_ms;
