package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// ErrAlreadySetUp is returned by CreateFirstOwner when any user exists.
var ErrAlreadySetUp = errors.New("already set up")

// setupLockKey is the transaction-scoped advisory lock that serializes
// first-owner creation. Arbitrary but fixed; only CreateFirstOwner takes it.
const setupLockKey = 0x486f6d6548756201 // "HomeHub\x01"

// CreateFirstOwner creates the initial account and grants it roleName, but
// only while the users table is empty. The emptiness check and the insert run
// under one advisory lock inside one transaction, so two simultaneous setup
// submissions can't both observe "no users" and both become Owner: the second
// blocks until the first commits, then sees a user and gets ErrAlreadySetUp.
func (s *Store) CreateFirstOwner(ctx context.Context, username, displayName, passwordHash, roleName string) (*User, error) {
	var user *User
	err := s.WithTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock($1)`, int64(setupLockKey)); err != nil {
			return err
		}
		var exists bool
		if err := tx.QueryRow(ctx, `select exists (select 1 from users)`).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return ErrAlreadySetUp
		}
		u, err := scanOne[User](tx.Query(ctx, `
			insert into users (username, display_name, password_hash, must_change_password)
			values ($1, $2, $3, false)
			returning `+userCols,
			username, displayName, passwordHash))
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
			insert into user_roles (user_id, role_id)
			select $1, id from roles where lower(name) = lower($2)`, u.ID, roleName)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			// An owner without the Owner role would be a locked-out instance.
			return errors.New("owner role missing")
		}
		user = u
		return nil
	})
	if err != nil {
		return nil, err
	}
	return user, nil
}
