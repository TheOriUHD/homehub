-- +goose NO TRANSACTION
-- +goose Up
-- The 5-minute rollup job selects metric_samples by time window across all
-- series (ts >= $since and ts < $until), and retention deletes by age; the
-- only index led with series_id, so both scanned the whole raw table every
-- five minutes. Rollup retention filters metric_rollups by bucket and age,
-- which its (series_id, bucket, ts) key cannot serve either. Built
-- concurrently so an upgrade does not block metric writes on a large table.
create index concurrently if not exists metric_samples_ts_idx on metric_samples (ts);
create index concurrently if not exists metric_rollups_bucket_ts_idx on metric_rollups (bucket, ts);

-- +goose Down
drop index concurrently if exists metric_rollups_bucket_ts_idx;
drop index concurrently if exists metric_samples_ts_idx;
