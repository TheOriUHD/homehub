package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// SignalNotice records that a condition has already been reported outward,
// and whether an all-clear for it is waiting to be announced.
type SignalNotice struct {
	Key              string     `db:"key"`
	LastSentAt       time.Time  `db:"last_sent_at"`
	LastSeverity     string     `db:"last_severity"`
	SentCount        int        `db:"sent_count"`
	RecoveryDueAt    *time.Time `db:"recovery_due_at"`
	RecoverySignalID *int64     `db:"recovery_signal_id"`
	// The last fault reported, kept apart from the last message of any kind
	// so a return after an all-clear is judged against what was reported.
	LastFaultAt       *time.Time `db:"last_fault_at"`
	LastFaultSeverity string     `db:"last_fault_severity"`
	// Flaps counts fault→clear→fault cycles inside the flap window.
	Flaps           int        `db:"flaps"`
	FlapWindowStart *time.Time `db:"flap_window_start"`
}

const signalNoticeCols = `key, last_sent_at, last_severity, sent_count, recovery_due_at, recovery_signal_id,
	last_fault_at, last_fault_severity, flaps, flap_window_start`

// GetSignalNotice returns what has already been said about a signal key, or
// nil when nothing has. Not finding one is the normal first-time case, not an
// error the caller should have to distinguish.
func (s *Store) GetSignalNotice(ctx context.Context, key string) (*SignalNotice, error) {
	row, err := scanOne[SignalNotice](s.pool.Query(ctx,
		`select `+signalNoticeCols+` from signal_notices where key = $1`, key))
	if errors.Is(err, ErrNotFound) || errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return row, err
}

// ScheduleRecovery parks an all-clear for a reported condition until due.
// Only conditions the owner has heard about get one: a recovery nobody was
// told to expect is not news. Reports whether anything was scheduled.
func (s *Store) ScheduleRecovery(ctx context.Context, key string, signalID int64, due time.Time) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		update signal_notices set recovery_due_at = $2, recovery_signal_id = $3
		where key = $1 and last_severity <> 'recovered'`, key, due, signalID)
	return tag.RowsAffected() > 0, err
}

// CancelRecovery withdraws a parked all-clear — the fault came back before
// it was announced. Reports whether one was waiting.
func (s *Store) CancelRecovery(ctx context.Context, key string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		update signal_notices set recovery_due_at = null, recovery_signal_id = null
		where key = $1 and recovery_due_at is not null`, key)
	return tag.RowsAffected() > 0, err
}

// DueRecoveries returns the parked all-clears whose wait is over, and clears
// them in the same statement so a second flush cannot announce them again.
func (s *Store) DueRecoveries(ctx context.Context) ([]SignalNotice, error) {
	return scanMany[SignalNotice](s.pool.Query(ctx, `
		update signal_notices set recovery_due_at = null
		where recovery_due_at <= now()
		returning `+signalNoticeCols))
}

// RecordSignalNotice marks a condition as reported, now.
// RecordSignalNotice records that a message went out for a key. A fault
// (anything but "recovered") also becomes the last fault on record, and if it
// follows an all-clear inside flapWindow it counts as a flap — the window
// starts at the first flap and resets once a fault arrives after a quiet
// stretch longer than the window.
func (s *Store) RecordSignalNotice(ctx context.Context, key, severity string, flapWindow time.Duration) error {
	_, err := s.pool.Exec(ctx, `
		insert into signal_notices (key, last_sent_at, last_severity, sent_count, last_fault_at, last_fault_severity, flaps, flap_window_start)
		values ($1, now(), $2, 1,
			case when $2 <> 'recovered' then now() end,
			case when $2 <> 'recovered' then $2 else '' end,
			0, null)
		on conflict (key) do update set
			last_sent_at = now(),
			last_severity = $2,
			sent_count = signal_notices.sent_count + 1,
			last_fault_at = case when $2 <> 'recovered' then now() else signal_notices.last_fault_at end,
			last_fault_severity = case when $2 <> 'recovered' then $2 else signal_notices.last_fault_severity end,
			flaps = case
				when $2 = 'recovered' then signal_notices.flaps
				when signal_notices.last_severity = 'recovered'
				 and signal_notices.last_fault_at is not null
				 and signal_notices.last_fault_at > now() - $3::interval
					then signal_notices.flaps + 1
				when signal_notices.last_fault_at is null or signal_notices.last_fault_at <= now() - $3::interval
					then 0
				else signal_notices.flaps end,
			flap_window_start = case
				when $2 = 'recovered' then signal_notices.flap_window_start
				when signal_notices.last_severity = 'recovered'
				 and signal_notices.last_fault_at is not null
				 and signal_notices.last_fault_at > now() - $3::interval
					then coalesce(signal_notices.flap_window_start, now())
				when signal_notices.last_fault_at is null or signal_notices.last_fault_at <= now() - $3::interval
					then null
				else signal_notices.flap_window_start end`,
		key, severity, flapWindow.String())
	return err
}

// SentNotice is one message that went out about a condition.
type SentNotice struct {
	ID         int64     `db:"id"`
	Key        string    `db:"key"`
	Event      string    `db:"event"`
	Severity   string    `db:"severity"`
	Title      string    `db:"title"`
	SourceName string    `db:"source_name"`
	Text       string    `db:"text"`
	SentAt     time.Time `db:"sent_at"`
}

// LogSentNotice keeps the message that actually went out.
func (s *Store) LogSentNotice(ctx context.Context, n SentNotice) error {
	_, err := s.pool.Exec(ctx, `
		insert into signal_notice_log (key, event, severity, title, source_name, text)
		values ($1, $2, $3, $4, $5, $6)`,
		n.Key, n.Event, n.Severity, n.Title, n.SourceName, n.Text)
	return err
}

// ListSentNotices returns messages sent since a time, newest first. contains
// narrows to messages mentioning a phrase (in the text, title or source),
// case-insensitively; empty matches all.
func (s *Store) ListSentNotices(ctx context.Context, since time.Time, contains string, limit int) ([]SentNotice, error) {
	return scanMany[SentNotice](s.pool.Query(ctx, `
		select id, key, event, severity, title, source_name, text, sent_at
		from signal_notice_log
		where sent_at >= $1
		  and ($2 = '' or text ilike $4 escape '\' or title ilike $4 escape '\' or source_name ilike $4 escape '\')
		order by sent_at desc limit $3`, since, contains, limit, likeContains(contains)))
}

// PruneSentNotices ages the log out with the notices themselves.
func (s *Store) PruneSentNotices(ctx context.Context, olderThan time.Duration) (int64, error) {
	tag, err := s.pool.Exec(ctx,
		`delete from signal_notice_log where sent_at < now() - $1::interval`, olderThan.String())
	return tag.RowsAffected(), err
}

// PruneSignalNotices drops records for conditions nobody has heard about in a
// long while, so a fault that returns months later is announced as news.
// Recoveries deliberately do NOT clear a record: clearing on the all-clear is
// what let a flapping connection re-announce itself every few minutes, each
// firing looking like the first.
func (s *Store) PruneSignalNotices(ctx context.Context, olderThan time.Duration) (int64, error) {
	tag, err := s.pool.Exec(ctx,
		`delete from signal_notices where last_sent_at < now() - $1::interval`, olderThan.String())
	return tag.RowsAffected(), err
}
