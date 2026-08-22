-- +goose Up
-- What an answer cost to produce: the model that wrote it and the tokens it
-- consumed, summed across every round of that turn's tool loop. Null on rows
-- written before this existed, which the UI reads as "unknown" rather than
-- as zero — a free answer and an unmeasured one are different facts.
alter table assistant_messages
    add column model text not null default '',
    add column input_tokens bigint,
    add column output_tokens bigint;

-- +goose Down
alter table assistant_messages
    drop column if exists model,
    drop column if exists input_tokens,
    drop column if exists output_tokens;
