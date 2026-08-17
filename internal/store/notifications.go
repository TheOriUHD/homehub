package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type Notification struct {
	ID        int64      `db:"id"`
	UserID    uuid.UUID  `db:"user_id"`
	TS        time.Time  `db:"ts"`
	Severity  string     `db:"severity"`
	Title     string     `db:"title"`
	Body      string     `db:"body"`
	Resource  string     `db:"resource"`
	EventType string     `db:"event_type"`
	ReadAt    *time.Time `db:"read_at"`
}

func (s *Store) InsertNotification(ctx context.Context, n *Notification) (*Notification, error) {
	return scanOne[Notification](s.pool.Query(ctx, `
		insert into notifications (user_id, severity, title, body, resource, event_type)
		values ($1, $2, $3, $4, $5, $6)
		returning id, user_id, ts, severity, title, body, resource, event_type, read_at`,
		n.UserID, n.Severity, n.Title, n.Body, n.Resource, n.EventType))
}

// ListNotifications pages newest-first with a keyset cursor (id descending).
func (s *Store) ListNotifications(ctx context.Context, userID uuid.UUID, unreadOnly bool, beforeID *int64, limit int) ([]Notification, error) {
	return scanMany[Notification](s.pool.Query(ctx, `
		select id, user_id, ts, severity, title, body, resource, event_type, read_at
		from notifications
		where user_id = $1
		  and (not $2 or read_at is null)
		  and ($3::bigint is null or id < $3)
		order by id desc
		limit $4`,
		userID, unreadOnly, beforeID, limit))
}

func (s *Store) MarkNotificationsRead(ctx context.Context, userID uuid.UUID, ids []int64) error {
	_, err := s.pool.Exec(ctx,
		`update notifications set read_at = now() where user_id = $1 and id = any($2) and read_at is null`,
		userID, ids)
	return err
}

func (s *Store) MarkAllNotificationsRead(ctx context.Context, userID uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`update notifications set read_at = now() where user_id = $1 and read_at is null`, userID)
	return err
}

func (s *Store) UnreadNotificationCount(ctx context.Context, userID uuid.UUID) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`select count(*) from notifications where user_id = $1 and read_at is null`, userID).Scan(&n)
	return n, err
}

// PruneNotifications removes read notifications past the retention window.
func (s *Store) PruneNotifications(ctx context.Context, olderThan time.Duration) (int64, error) {
	tag, err := s.pool.Exec(ctx,
		`delete from notifications where read_at is not null and ts < now() - $1::interval`,
		olderThan.String())
	return tag.RowsAffected(), err
}
