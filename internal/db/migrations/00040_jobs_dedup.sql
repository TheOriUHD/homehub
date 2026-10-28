-- +goose Up
-- A job kind can name the "target" of its work (a backup target, a container,
-- a schedule). While one job for that target is pending or running, asking
-- again joins it instead of queueing a second copy: a schedule that fires
-- while its last run is still going, or a double-clicked backup button, can no
-- longer pile identical work up behind a slow run. The partial unique index is
-- what makes the check atomic across workers and API requests.
alter table jobs add column dedup_key text;
create unique index jobs_dedup_active_idx on jobs (dedup_key)
    where dedup_key is not null and status in ('pending', 'running');

-- +goose Down
drop index jobs_dedup_active_idx;
alter table jobs drop column dedup_key;
