-- +goose Up
-- Why Auto sent this turn where it did. Rule-based routing is only better
-- than a model grading its own confidence if the reason can be shown, so the
-- deciding rule is stored beside the model it chose. Null on turns where the
-- user pinned a model themselves.
alter table assistant_messages add column route text;

-- +goose Down
alter table assistant_messages drop column if exists route;
