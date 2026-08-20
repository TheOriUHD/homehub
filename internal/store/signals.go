package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Signal is one row of the global notification stream. See migration 00018
// for the lifecycle: keyed conditions update in place, flip to 'recovered'
// when they clear, and leave the live set only on dismissal.
type Signal struct {
	ID          int64           `db:"id"`
	Key         string          `db:"key"`
	Collector   string          `db:"collector"`
	SourceType  string          `db:"source_type"`
	SourceID    *uuid.UUID      `db:"source_id"`
	SourceName  string          `db:"source_name"`
	Severity    string          `db:"severity"`
	AutoResolve bool            `db:"auto_resolve"`
	Title       string          `db:"title"`
	Body        string          `db:"body"`
	Detail      json.RawMessage `db:"detail"`
	Count       int             `db:"count"`
	FirstSeen   time.Time       `db:"first_seen"`
	LastSeen    time.Time       `db:"last_seen"`
	ResolvedAt  *time.Time      `db:"resolved_at"`
	DismissedAt *time.Time      `db:"dismissed_at"`
	DismissedBy *uuid.UUID      `db:"dismissed_by"`
}

const signalCols = `id, key, collector, source_type, source_id, source_name, severity, auto_resolve,
	title, body, detail, count, first_seen, last_seen, resolved_at, dismissed_at, dismissed_by`

// UpsertSignal reconciles one observation into the live set. A fresh key
// inserts; a live key updates severity/title/body/detail and increments count
// only when `recur` (a new occurrence of a one-shot event, not a still-true
// condition). last_seen bumps only when something the operator can SEE
// changed (severity/title/body, or a recurrence) — a still-true, unchanged
// condition keeps its timestamp, so the panel never shows a wall of
// "1 minute ago" rows that merely got re-observed by the sweep.
func (s *Store) UpsertSignal(ctx context.Context, sig Signal, recur bool) (*Signal, error) {
	return scanOne[Signal](s.pool.Query(ctx, `
		insert into signals (key, collector, source_type, source_id, source_name, severity, auto_resolve, title, body, detail)
		values ($1,$2,$3,$4,$5,$6,$11,$7,$8,$9)
		on conflict (key) where dismissed_at is null do update set
			severity    = excluded.severity,
			auto_resolve = excluded.auto_resolve,
			title       = excluded.title,
			body        = excluded.body,
			detail      = excluded.detail,
			source_name = excluded.source_name,
			count       = signals.count + case when $10 then 1 else 0 end,
			last_seen   = case when $10
				or signals.severity is distinct from excluded.severity
				or signals.title    is distinct from excluded.title
				or signals.body     is distinct from excluded.body
				then now() else signals.last_seen end,
			resolved_at = case when excluded.severity = 'recovered' then coalesce(signals.resolved_at, now()) else null end
		returning `+signalCols,
		sig.Key, sig.Collector, sig.SourceType, sig.SourceID, sig.SourceName,
		sig.Severity, sig.Title, sig.Body, sig.Detail, recur, sig.AutoResolve))
}

// ---- dismissal-as-acknowledgement --------------------------------------------
//
// Dismissing a signal whose condition is STILL TRUE must not make it pop back
// on the next sweep. The dismissed row stays behind as a suppression marker:
// re-observations of the same key at the same (or lower) severity update it
// quietly, an escalation reopens it, and when the condition finally clears it
// is marked resolved so a genuine re-occurrence later surfaces fresh.

// GetSuppressedSignal returns the newest dismissed-but-unresolved row for a
// key — the marker that says "the operator acknowledged this while it stood".
func (s *Store) GetSuppressedSignal(ctx context.Context, key string) (*Signal, error) {
	return scanOne[Signal](s.pool.Query(ctx,
		`select `+signalCols+` from signals
		 where key = $1 and dismissed_at is not null and resolved_at is null
		 order by dismissed_at desc limit 1`, key))
}

// TouchSuppressedSignal keeps an acknowledged, still-true condition current
// (and un-prunable) without resurfacing it.
func (s *Store) TouchSuppressedSignal(ctx context.Context, id int64, severity, title, body string, detail json.RawMessage) error {
	_, err := s.pool.Exec(ctx, `
		update signals set severity = $2, title = $3, body = $4, detail = coalesce($5, detail), last_seen = now()
		where id = $1 and dismissed_at is not null`, id, severity, title, body, detail)
	return err
}

// ReopenSignal resurfaces an acknowledged row — the condition escalated past
// the severity the operator dismissed. Caller must ensure no live row exists
// for the key (the partial unique index allows one undismissed row per key).
func (s *Store) ReopenSignal(ctx context.Context, id int64, severity, title, body string, detail json.RawMessage) (*Signal, error) {
	return scanOne[Signal](s.pool.Query(ctx, `
		update signals set dismissed_at = null, dismissed_by = null,
			severity = $2, title = $3, body = $4, detail = coalesce($5, detail),
			count = count + 1, last_seen = now(), resolved_at = null
		where id = $1
		returning `+signalCols, id, severity, title, body, detail))
}

// ListSuppressedSignalKeys returns the acknowledged, still-unresolved,
// self-clearing keys for one collector — reconciled alongside the live set so
// suppression ends when the condition does.
func (s *Store) ListSuppressedSignalKeys(ctx context.Context, collector string) (map[string]bool, error) {
	rows, err := s.pool.Query(ctx,
		`select key from signals
		 where collector = $1 and dismissed_at is not null and resolved_at is null and auto_resolve`, collector)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out[k] = true
	}
	return out, rows.Err()
}

// ResolveSuppressedSignal quietly closes an acknowledged condition that has
// cleared, re-arming the key for a genuine future occurrence.
func (s *Store) ResolveSuppressedSignal(ctx context.Context, key string) error {
	_, err := s.pool.Exec(ctx, `
		update signals set severity = 'recovered', resolved_at = now()
		where key = $1 and dismissed_at is not null and resolved_at is null`, key)
	return err
}

// GetLiveSignal fetches the undismissed row for a key, if any.
func (s *Store) GetLiveSignal(ctx context.Context, key string) (*Signal, error) {
	return scanOne[Signal](s.pool.Query(ctx,
		`select `+signalCols+` from signals where key = $1 and dismissed_at is null`, key))
}

// ListLiveSignalKeys returns the keys of undismissed, unresolved, self-
// clearing signals for one collector — the reconciliation set for
// auto-resolve. Survives restarts because the flag lives in the row.
func (s *Store) ListLiveSignalKeys(ctx context.Context, collector string) (map[string]bool, error) {
	rows, err := s.pool.Query(ctx,
		`select key from signals
		 where collector = $1 and dismissed_at is null and severity <> 'recovered' and auto_resolve`, collector)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out[k] = true
	}
	return out, rows.Err()
}

// ResolveSignal flips a live signal to 'recovered' (green) — the condition
// cleared on its own. It stays listed until dismissed.
func (s *Store) ResolveSignal(ctx context.Context, key, title, body string) (*Signal, error) {
	return scanOne[Signal](s.pool.Query(ctx, `
		update signals set severity = 'recovered',
			title = case when $2 <> '' then $2 else title end,
			body  = case when $3 <> '' then $3 else body end,
			resolved_at = now(), last_seen = now()
		where key = $1 and dismissed_at is null
		returning `+signalCols, key, title, body))
}

// ListSignals returns the live (undismissed) stream newest-activity-first.
func (s *Store) ListSignals(ctx context.Context, limit int) ([]Signal, error) {
	return scanMany[Signal](s.pool.Query(ctx,
		`select `+signalCols+` from signals where dismissed_at is null order by last_seen desc limit $1`, limit))
}

// SignalCounts returns live counts per severity.
func (s *Store) SignalCounts(ctx context.Context) (map[string]int, error) {
	rows, err := s.pool.Query(ctx,
		`select severity, count(*) from signals where dismissed_at is null group by severity`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var sev string
		var n int
		if err := rows.Scan(&sev, &n); err != nil {
			return nil, err
		}
		out[sev] = n
	}
	return out, rows.Err()
}

// DismissSignals dismisses by id; returns how many were live.
func (s *Store) DismissSignals(ctx context.Context, ids []int64, by uuid.UUID) (int64, error) {
	tag, err := s.pool.Exec(ctx,
		`update signals set dismissed_at = now(), dismissed_by = $2 where id = any($1) and dismissed_at is null`,
		ids, by)
	return tag.RowsAffected(), err
}

// PruneSignals removes dismissed rows past the retention window.
func (s *Store) PruneSignals(ctx context.Context, olderThan time.Duration) (int64, error) {
	tag, err := s.pool.Exec(ctx,
		`delete from signals where dismissed_at is not null and last_seen < now() - $1::interval`,
		olderThan.String())
	return tag.RowsAffected(), err
}
