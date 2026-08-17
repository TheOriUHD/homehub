package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type Invite struct {
	ID        uuid.UUID  `db:"id"`
	UserID    uuid.UUID  `db:"user_id"`
	TokenHash []byte     `db:"token_hash"`
	CreatedBy *uuid.UUID `db:"created_by"`
	ExpiresAt time.Time  `db:"expires_at"`
	UsedAt    *time.Time `db:"used_at"`
	CreatedAt time.Time  `db:"created_at"`
}

// CreateInvite records an invite link for a user, replacing any previous
// outstanding invite (one live invite per user).
func (s *Store) CreateInvite(ctx context.Context, userID uuid.UUID, tokenHash []byte, createdBy uuid.UUID, expiresAt time.Time) error {
	_, err := s.pool.Exec(ctx, `delete from invites where user_id = $1 and used_at is null`, userID)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx,
		`insert into invites (user_id, token_hash, created_by, expires_at) values ($1, $2, $3, $4)`,
		userID, tokenHash, createdBy, expiresAt)
	return err
}

// InviteByTokenHash resolves a live invite and its target user.
func (s *Store) InviteByTokenHash(ctx context.Context, tokenHash []byte) (*Invite, *User, error) {
	row := s.pool.QueryRow(ctx, `
		select i.id, i.user_id, i.token_hash, i.created_by, i.expires_at, i.used_at, i.created_at,
		       u.id, u.username, u.display_name, u.email, u.password_hash, u.must_change_password,
		       u.status, u.failed_login_count, u.locked_until, u.password_changed_at, u.last_login_at,
		       u.created_at, u.updated_at
		from invites i
		join users u on u.id = i.user_id
		where i.token_hash = $1 and i.used_at is null and i.expires_at > now() and u.status = 'active'`,
		tokenHash)
	var inv Invite
	var u User
	err := row.Scan(
		&inv.ID, &inv.UserID, &inv.TokenHash, &inv.CreatedBy, &inv.ExpiresAt, &inv.UsedAt, &inv.CreatedAt,
		&u.ID, &u.Username, &u.DisplayName, &u.Email, &u.PasswordHash, &u.MustChangePassword,
		&u.Status, &u.FailedLoginCount, &u.LockedUntil, &u.PasswordChangedAt, &u.LastLoginAt,
		&u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, nil, notFound(err)
	}
	return &inv, &u, nil
}

func (s *Store) MarkInviteUsed(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		`update invites set used_at = now() where id = $1 and used_at is null`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
