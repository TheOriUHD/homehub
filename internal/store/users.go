package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type User struct {
	ID                 uuid.UUID  `db:"id"`
	Username           string     `db:"username"`
	DisplayName        string     `db:"display_name"`
	Email              *string    `db:"email"`
	PasswordHash       *string    `db:"password_hash"`
	MustChangePassword bool       `db:"must_change_password"`
	HideSignals        bool       `db:"hide_signals"`
	Status             string     `db:"status"`
	FailedLoginCount   int        `db:"failed_login_count"`
	LockedUntil        *time.Time `db:"locked_until"`
	PasswordChangedAt  *time.Time `db:"password_changed_at"`
	LastLoginAt        *time.Time `db:"last_login_at"`
	CreatedAt          time.Time  `db:"created_at"`
	UpdatedAt          time.Time  `db:"updated_at"`
}

const userCols = `id, username, display_name, email, password_hash, must_change_password,
	hide_signals, status, failed_login_count, locked_until, password_changed_at, last_login_at,
	created_at, updated_at`

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `select count(*) from users`).Scan(&n)
	return n, err
}

func (s *Store) CreateUser(ctx context.Context, username, displayName string, email *string, passwordHash *string, mustChange bool) (*User, error) {
	return scanOne[User](s.pool.Query(ctx, `
		insert into users (username, display_name, email, password_hash, must_change_password)
		values ($1, $2, $3, $4, $5)
		returning `+userCols,
		username, displayName, email, passwordHash, mustChange))
}

func (s *Store) GetUser(ctx context.Context, id uuid.UUID) (*User, error) {
	return scanOne[User](s.pool.Query(ctx, `select `+userCols+` from users where id = $1`, id))
}

func (s *Store) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	return scanOne[User](s.pool.Query(ctx, `select `+userCols+` from users where lower(username) = lower($1)`, username))
}

func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	return scanMany[User](s.pool.Query(ctx, `select `+userCols+` from users order by lower(username)`))
}

func (s *Store) SetUserProfile(ctx context.Context, id uuid.UUID, displayName string, email *string) error {
	_, err := s.pool.Exec(ctx,
		`update users set display_name = $2, email = $3, updated_at = now() where id = $1`,
		id, displayName, email)
	return err
}

// SetUserUsername renames the login. Uniqueness is case-insensitive
// (users_username_key on lower(username)) — the caller maps the violation.
func (s *Store) SetUserUsername(ctx context.Context, id uuid.UUID, username string) error {
	_, err := s.pool.Exec(ctx,
		`update users set username = $2, updated_at = now() where id = $1`,
		id, username)
	return err
}

func (s *Store) SetUserStatus(ctx context.Context, id uuid.UUID, status string) error {
	_, err := s.pool.Exec(ctx, `update users set status = $2, updated_at = now() where id = $1`, id, status)
	return err
}

// SetUserHideSignals toggles the per-user signals-visibility override.
func (s *Store) SetUserHideSignals(ctx context.Context, id uuid.UUID, hide bool) error {
	_, err := s.pool.Exec(ctx, `update users set hide_signals = $2, updated_at = now() where id = $1`, id, hide)
	return err
}

// SignalsHiddenForUser reports whether the signals surface should be hidden for
// a user: true when their own override is set, OR when they hold at least one
// role and EVERY role they hold hides signals (a single non-hiding role — e.g.
// Operator — keeps signals visible, matching how grants are most-permissive).
func (s *Store) SignalsHiddenForUser(ctx context.Context, userID uuid.UUID) (bool, error) {
	var hidden bool
	err := s.pool.QueryRow(ctx, `
		select u.hide_signals
		   or (exists (select 1 from user_roles ur where ur.user_id = u.id)
		       and not exists (
		           select 1 from user_roles ur
		           join roles r on r.id = ur.role_id
		           where ur.user_id = u.id and r.hide_signals = false))
		from users u where u.id = $1`, userID).Scan(&hidden)
	return hidden, err
}

// SetUserPassword stores a new hash, clears lockout state, and stamps
// password_changed_at.
func (s *Store) SetUserPassword(ctx context.Context, id uuid.UUID, hash string, mustChange bool) error {
	_, err := s.pool.Exec(ctx, `
		update users
		set password_hash = $2, must_change_password = $3, password_changed_at = now(),
		    failed_login_count = 0, locked_until = null, updated_at = now()
		where id = $1`, id, hash, mustChange)
	return err
}

func (s *Store) DeleteUser(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `delete from users where id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// RecordLoginSuccess clears failure counters and stamps last_login_at.
func (s *Store) RecordLoginSuccess(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `
		update users set failed_login_count = 0, locked_until = null, last_login_at = now(), updated_at = now()
		where id = $1`, id)
	return err
}

// CountLoginFailure increments the account's failure counter without locking
// it. Lockout lives in the auth service, keyed per client IP, so a remote
// party can't lock someone else out.
func (s *Store) CountLoginFailure(ctx context.Context, id uuid.UUID) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `
		update users set failed_login_count = failed_login_count + 1, updated_at = now()
		where id = $1
		returning failed_login_count`, id).Scan(&count)
	return count, err
}

// UserRoles returns role IDs held by a user.
func (s *Store) UserRoleIDs(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.pool.Query(ctx, `select role_id from user_roles where user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

// SetUserRoles replaces a user's role set.
func (s *Store) SetUserRoles(ctx context.Context, userID uuid.UUID, roleIDs []uuid.UUID) error {
	return s.WithTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `delete from user_roles where user_id = $1`, userID); err != nil {
			return err
		}
		for _, rid := range roleIDs {
			if _, err := tx.Exec(ctx,
				`insert into user_roles (user_id, role_id) values ($1, $2) on conflict do nothing`,
				userID, rid); err != nil {
				return err
			}
		}
		return nil
	})
}
