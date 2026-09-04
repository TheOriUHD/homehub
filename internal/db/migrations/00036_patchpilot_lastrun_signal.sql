-- +goose Up
-- The PatchPilot collector now keeps one signal per fleet about the most
-- recent run (key pp:<fleet>:lastrun) instead of one per failed run in the
-- last two days. The per-run rows it used to emit would otherwise sit in the
-- stream forever — they were one-shots waiting for a dismissal that never
-- came — so they are acknowledged here, once.
update signals set dismissed_at = now()
where key like 'pp:%:run:%' and dismissed_at is null;

-- +goose Down
-- Nothing to undo: the rows were acknowledged, not removed.
