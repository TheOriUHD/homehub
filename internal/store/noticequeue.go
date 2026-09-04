package store

import (
	"context"
	"encoding/json"
	"time"
)

// QueuedNotice is one condition waiting for the digest.
type QueuedNotice struct {
	ID         int64           `db:"id"`
	Key        string          `db:"key"`
	SignalID   *int64          `db:"signal_id"`
	Event      string          `db:"event"`
	Severity   string          `db:"severity"`
	Title      string          `db:"title"`
	Body       string          `db:"body"`
	SourceName string          `db:"source_name"`
	SourceType string          `db:"source_type"`
	Detail     json.RawMessage `db:"detail"`
	Bounces    int             `db:"bounces"`
	QueuedAt   time.Time       `db:"queued_at"`
	UpdatedAt  time.Time       `db:"updated_at"`
	ResolvedAt *time.Time      `db:"resolved_at"`
	FlushedAt  *time.Time      `db:"flushed_at"`
	FlushKind  string          `db:"flush_kind"`
}

const noticeQueueCols = `id, key, signal_id, event, severity, title, body, source_name, source_type, detail,
	bounces, queued_at, updated_at, resolved_at, flushed_at, flush_kind`

// EnqueueNotice adds a condition to the digest, or refreshes the live row for
// its key: the latest severity and text win, and a fault that had already
// cleared while waiting counts as a bounce.
func (s *Store) EnqueueNotice(ctx context.Context, n QueuedNotice) (*QueuedNotice, error) {
	return scanOne[QueuedNotice](s.pool.Query(ctx, `
		insert into notice_queue (key, signal_id, event, severity, title, body, source_name, source_type, detail)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		on conflict (key) where flushed_at is null do update set
			signal_id   = excluded.signal_id,
			event       = excluded.event,
			severity    = excluded.severity,
			title       = excluded.title,
			body        = excluded.body,
			source_name = excluded.source_name,
			detail      = excluded.detail,
			bounces     = notice_queue.bounces + case when notice_queue.resolved_at is not null then 1 else 0 end,
			resolved_at = null,
			updated_at  = now()
		returning `+noticeQueueCols,
		n.Key, n.SignalID, n.Event, n.Severity, n.Title, n.Body, n.SourceName, n.SourceType, n.Detail))
}

// ResolveQueuedNotice marks the live queued item for a key as cleared; it
// stays in the digest as something that came and went. Reports whether there
// was one.
func (s *Store) ResolveQueuedNotice(ctx context.Context, key string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		update notice_queue set resolved_at = now(), updated_at = now()
		where key = $1 and flushed_at is null and resolved_at is null`, key)
	return tag.RowsAffected() > 0, err
}

// ListQueuedNotices returns everything waiting, worst first, oldest first
// within a severity.
func (s *Store) ListQueuedNotices(ctx context.Context) ([]QueuedNotice, error) {
	return scanMany[QueuedNotice](s.pool.Query(ctx, `
		select `+noticeQueueCols+` from notice_queue where flushed_at is null
		order by case severity when 'critical' then 0 when 'warning' then 1 when 'info' then 2 else 3 end, queued_at`))
}

// CountQueuedNotices is the badge number.
func (s *Store) CountQueuedNotices(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `select count(*) from notice_queue where flushed_at is null`).Scan(&n)
	return n, err
}

// FlushQueuedNotices stamps rows as sent, by digest or tagged onto an
// immediate message.
func (s *Store) FlushQueuedNotices(ctx context.Context, ids []int64, kind string) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := s.pool.Exec(ctx, `update notice_queue set flushed_at = now(), flush_kind = $2 where id = any($1) and flushed_at is null`, ids, kind)
	return err
}

// PruneNoticeQueue removes flushed rows past the retention window.
func (s *Store) PruneNoticeQueue(ctx context.Context, olderThan time.Duration) (int64, error) {
	tag, err := s.pool.Exec(ctx, `delete from notice_queue where flushed_at is not null and flushed_at < now() - $1::interval`, olderThan.String())
	return tag.RowsAffected(), err
}
