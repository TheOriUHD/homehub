package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// SignalNotice records that a condition has already been reported outward.
type SignalNotice struct {
	Key          string    `db:"key"`
	LastSentAt   time.Time `db:"last_sent_at"`
	LastSeverity string    `db:"last_severity"`
	SentCount    int       `db:"sent_count"`
}

// GetSignalNotice returns what has already been said about a signal key, or
// nil when nothing has. Not finding one is the normal first-time case, not an
// error the caller should have to distinguish.
func (s *Store) GetSignalNotice(ctx context.Context, key string) (*SignalNotice, error) {
	row, err := scanOne[SignalNotice](s.pool.Query(ctx,
		`select key, last_sent_at, last_severity, sent_count from signal_notices where key = $1`, key))
	if errors.Is(err, ErrNotFound) || errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return row, err
}

// RecordSignalNotice marks a condition as reported, now.
func (s *Store) RecordSignalNotice(ctx context.Context, key, severity string) error {
	_, err := s.pool.Exec(ctx, `
		insert into signal_notices (key, last_sent_at, last_severity, sent_count)
		values ($1, now(), $2, 1)
		on conflict (key) do update
		set last_sent_at = now(), last_severity = $2, sent_count = signal_notices.sent_count + 1`,
		key, severity)
	return err
}

// ClearSignalNotice forgets a condition once it recovers, so the next
// occurrence is announced as news rather than as a repeat.
func (s *Store) ClearSignalNotice(ctx context.Context, key string) error {
	_, err := s.pool.Exec(ctx, `delete from signal_notices where key = $1`, key)
	return err
}
