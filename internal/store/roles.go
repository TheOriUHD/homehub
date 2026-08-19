package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Role struct {
	ID          uuid.UUID `db:"id"`
	Name        string    `db:"name"`
	Description string    `db:"description"`
	IsSystem    bool      `db:"is_system"`
	HideSignals bool      `db:"hide_signals"`
	CreatedAt   time.Time `db:"created_at"`
	UpdatedAt   time.Time `db:"updated_at"`
}

type Grant struct {
	ID       uuid.UUID  `db:"id"`
	RoleID   *uuid.UUID `db:"role_id"`
	UserID   *uuid.UUID `db:"user_id"`
	Selector string     `db:"selector"`
	Levels   []string   `db:"levels"`
	Effect   string     `db:"effect"`
}

type GrantSpec struct {
	Selector string
	Levels   []string
	Effect   string
}

func (s *Store) ListRoles(ctx context.Context) ([]Role, error) {
	return scanMany[Role](s.pool.Query(ctx,
		`select id, name, description, is_system, hide_signals, created_at, updated_at from roles order by is_system desc, lower(name)`))
}

func (s *Store) GetRole(ctx context.Context, id uuid.UUID) (*Role, error) {
	return scanOne[Role](s.pool.Query(ctx,
		`select id, name, description, is_system, hide_signals, created_at, updated_at from roles where id = $1`, id))
}

func (s *Store) GetRoleByName(ctx context.Context, name string) (*Role, error) {
	return scanOne[Role](s.pool.Query(ctx,
		`select id, name, description, is_system, hide_signals, created_at, updated_at from roles where lower(name) = lower($1)`, name))
}

func (s *Store) CreateRole(ctx context.Context, name, description string, isSystem bool, grants []GrantSpec) (*Role, error) {
	var role *Role
	err := s.WithTx(ctx, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx, `
			insert into roles (name, description, is_system) values ($1, $2, $3)
			returning id, name, description, is_system, hide_signals, created_at, updated_at`,
			name, description, isSystem)
		var r Role
		if err := row.Scan(&r.ID, &r.Name, &r.Description, &r.IsSystem, &r.HideSignals, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return err
		}
		role = &r
		return insertGrants(ctx, tx, &r.ID, nil, grants)
	})
	return role, err
}

func (s *Store) UpdateRole(ctx context.Context, id uuid.UUID, name, description string, grants []GrantSpec) error {
	return s.WithTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`update roles set name = $2, description = $3, updated_at = now() where id = $1`,
			id, name, description)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		if _, err := tx.Exec(ctx, `delete from grants where role_id = $1`, id); err != nil {
			return err
		}
		return insertGrants(ctx, tx, &id, nil, grants)
	})
}

// SetRoleHideSignals toggles whether members of this role see the signals
// surface. Allowed on system roles too (it is a display preference, not a
// grant), so an admin can hide signals from e.g. the built-in Viewer role.
func (s *Store) SetRoleHideSignals(ctx context.Context, id uuid.UUID, hide bool) error {
	tag, err := s.pool.Exec(ctx,
		`update roles set hide_signals = $2, updated_at = now() where id = $1`, id, hide)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteRole(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `delete from roles where id = $1 and not is_system`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) RoleGrants(ctx context.Context, roleID uuid.UUID) ([]Grant, error) {
	return scanMany[Grant](s.pool.Query(ctx,
		`select id, role_id, user_id, selector, levels, effect from grants where role_id = $1 order by selector`, roleID))
}

func (s *Store) RoleMemberCounts(ctx context.Context) (map[uuid.UUID]int, error) {
	rows, err := s.pool.Query(ctx, `select role_id, count(*) from user_roles group by role_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[uuid.UUID]int{}
	for rows.Next() {
		var id uuid.UUID
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		counts[id] = n
	}
	return counts, rows.Err()
}

// UserGrants returns the union of grants from every role the user holds plus
// the user's own overrides — the full input to permission evaluation.
func (s *Store) UserGrants(ctx context.Context, userID uuid.UUID) ([]Grant, error) {
	return scanMany[Grant](s.pool.Query(ctx, `
		select g.id, g.role_id, g.user_id, g.selector, g.levels, g.effect
		from grants g
		where g.user_id = $1
		   or g.role_id in (select role_id from user_roles where user_id = $1)`,
		userID))
}

// RoleGrantsOnly returns grants for a single role — used by "view as role".
func (s *Store) RoleGrantsOnly(ctx context.Context, roleID uuid.UUID) ([]Grant, error) {
	return scanMany[Grant](s.pool.Query(ctx,
		`select id, role_id, user_id, selector, levels, effect from grants where role_id = $1`, roleID))
}

// ReplaceUserGrants swaps a user's override grants.
func (s *Store) ReplaceUserGrants(ctx context.Context, userID uuid.UUID, grants []GrantSpec) error {
	return s.WithTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `delete from grants where user_id = $1`, userID); err != nil {
			return err
		}
		return insertGrants(ctx, tx, nil, &userID, grants)
	})
}

func (s *Store) UserOverrideGrants(ctx context.Context, userID uuid.UUID) ([]Grant, error) {
	return scanMany[Grant](s.pool.Query(ctx,
		`select id, role_id, user_id, selector, levels, effect from grants where user_id = $1 order by selector`, userID))
}

func insertGrants(ctx context.Context, tx pgx.Tx, roleID, userID *uuid.UUID, grants []GrantSpec) error {
	for _, g := range grants {
		effect := g.Effect
		if effect == "" {
			effect = "allow"
		}
		if _, err := tx.Exec(ctx,
			`insert into grants (role_id, user_id, selector, levels, effect) values ($1, $2, $3, $4, $5)`,
			roleID, userID, g.Selector, g.Levels, effect); err != nil {
			return err
		}
	}
	return nil
}
