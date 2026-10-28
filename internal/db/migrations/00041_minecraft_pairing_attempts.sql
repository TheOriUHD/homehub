-- +goose Up
-- A wrong pairing code is a guess against every live code (codes are one
-- global 6-digit space), so each failed attempt is counted on all of them and
-- a code that has absorbed too many is withdrawn. Together with the per-IP and
-- global attempt limits in the companion endpoint this bounds the chance of
-- guessing a code during its 15-minute life to a few in a million.
alter table minecraft_pairings add column failed_attempts int not null default 0;

-- +goose Down
alter table minecraft_pairings drop column failed_attempts;
