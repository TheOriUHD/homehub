package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type Session struct {
	ID           uuid.UUID  `db:"id"`
	UserID       uuid.UUID  `db:"user_id"`
	CreatedAt    time.Time  `db:"created_at"`
	LastSeenAt   time.Time  `db:"last_seen_at"`
	ExpiresAt    time.Time  `db:"expires_at"`
	IP           string     `db:"ip"`
	UserAgent    string     `db:"user_agent"`
	RevokedAt    *time.Time `db:"revoked_at"`
	ViewAsRoleID *uuid.UUID `db:"view_as_role_id"`
}

const sessionCols = `id, user_id, created_at, last_seen_at, expires_at, ip, user_agent, revoked_at, view_as_role_id`

func (s *Store) CreateSession(ctx context.Context, userID uuid.UUID, tokenHash []byte, expiresAt time.Time, ip, userAgent string) (*Session, error) {
	return scanOne[Session](s.pool.Query(ctx, `
		insert into sessions (user_id, token_hash, expires_at, ip, user_agent)
		values ($1, $2, $3, $4, $5)
		returning `+sessionCols,
		userID, tokenHash, expiresAt, ip, userAgent))
}

// SessionByTokenHash resolves a live (unrevoked, unexpired) session and its
// user in one round trip. Returns ErrNotFound for anything not usable.
func (s *Store) SessionByTokenHash(ctx context.Context, tokenHash []byte) (*Session, *User, error) {
	row := s.pool.QueryRow(ctx, `
		select s.id, s.user_id, s.created_at, s.last_seen_at, s.expires_at, s.ip, s.user_agent,
		       s.revoked_at, s.view_as_role_id,
		       u.id, u.username, u.display_name, u.email, u.password_hash, u.must_change_password,
		       u.status, u.failed_login_count, u.locked_until, u.password_changed_at, u.last_login_at,
		       u.created_at, u.updated_at
		from sessions s
		join users u on u.id = s.user_id
		where s.token_hash = $1
		  and s.revoked_at is null
		  and s.expires_at > now()
		  and u.status = 'active'`,
		tokenHash)
	var sess Session
	var u User
	err := row.Scan(
		&sess.ID, &sess.UserID, &sess.CreatedAt, &sess.LastSeenAt, &sess.ExpiresAt, &sess.IP, &sess.UserAgent,
		&sess.RevokedAt, &sess.ViewAsRoleID,
		&u.ID, &u.Username, &u.DisplayName, &u.Email, &u.PasswordHash, &u.MustChangePassword,
		&u.Status, &u.FailedLoginCount, &u.LockedUntil, &u.PasswordChangedAt, &u.LastLoginAt,
		&u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, nil, notFound(err)
	}
	return &sess, &u, nil
}

// TouchSession refreshes last-seen and (rolling expiry) pushes expires_at out.
func (s *Store) TouchSession(ctx context.Context, id uuid.UUID, ip string, expiresAt time.Time) error {
	_, err := s.pool.Exec(ctx,
		`update sessions set last_seen_at = now(), ip = $2, expires_at = $3 where id = $1`,
		id, ip, expiresAt)
	return err
}

func (s *Store) SetSessionViewAs(ctx context.Context, id uuid.UUID, roleID *uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `update sessions set view_as_role_id = $2 where id = $1`, id, roleID)
	return err
}

func (s *Store) RevokeSession(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		`update sessions set revoked_at = now() where id = $1 and revoked_at is null`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// RevokeUserSessions revokes all of a user's sessions, optionally sparing one
// (the caller's own).
func (s *Store) RevokeUserSessions(ctx context.Context, userID uuid.UUID, except *uuid.UUID) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		update sessions set revoked_at = now()
		where user_id = $1 and revoked_at is null and ($2::uuid is null or id <> $2)`,
		userID, except)
	return tag.RowsAffected(), err
}

func (s *Store) GetSession(ctx context.Context, id uuid.UUID) (*Session, error) {
	return scanOne[Session](s.pool.Query(ctx,
		`select `+sessionCols+` from sessions where id = $1`, id))
}

// ListUserSessions returns live sessions, newest activity first.
func (s *Store) ListUserSessions(ctx context.Context, userID uuid.UUID) ([]Session, error) {
	return scanMany[Session](s.pool.Query(ctx, `
		select `+sessionCols+` from sessions
		where user_id = $1 and revoked_at is null and expires_at > now()
		order by last_seen_at desc`, userID))
}

// CleanupSessions removes rows that are expired or long-revoked.
func (s *Store) CleanupSessions(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		delete from sessions
		where expires_at < now() - interval '7 days'
		   or revoked_at < now() - interval '7 days'`)
	return tag.RowsAffected(), err
}
