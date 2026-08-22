-- +goose Up
-- The turn's cost in US dollars, priced when it ran. Computed at write time
-- rather than derived on read: with prompt caching the same token counts bill
-- at three different rates, and the split is not recoverable from a total.
-- It also pins the price to the rates in force that day.
alter table assistant_messages add column cost_usd double precision;

-- +goose Down
alter table assistant_messages drop column if exists cost_usd;
