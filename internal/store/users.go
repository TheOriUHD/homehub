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
	Status             string     `db:"status"`
	FailedLoginCount   int        `db:"failed_login_count"`
	LockedUntil        *time.Time `db:"locked_until"`
	PasswordChangedAt  *time.Time `db:"password_changed_at"`
	LastLoginAt        *time.Time `db:"last_login_at"`
	CreatedAt          time.Time  `db:"created_at"`
	UpdatedAt          time.Time  `db:"updated_at"`
}

const userCols = `id, username, display_name, email, password_hash, must_change_password,
	status, failed_login_count, locked_until, password_changed_at, last_login_at,
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

// RecordLoginFailure increments the failure counter and, at or past the
// threshold, sets locked_until with exponential backoff. Returns the new
// counter and lock expiry.
func (s *Store) RecordLoginFailure(ctx context.Context, id uuid.UUID, threshold int, baseLock, maxLock time.Duration) (int, *time.Time, error) {
	var count int
	var lockedUntil *time.Time
	err := s.pool.QueryRow(ctx, `
		update users
		set failed_login_count = failed_login_count + 1,
		    locked_until = case
		        when failed_login_count + 1 >= $2 then
		            now() + least($4::interval, $3::interval * power(2, failed_login_count + 1 - $2))
		        else locked_until
		    end,
		    updated_at = now()
		where id = $1
		returning failed_login_count, locked_until`,
		id, threshold, baseLock.String(), maxLock.String()).Scan(&count, &lockedUntil)
	return count, lockedUntil, err
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
