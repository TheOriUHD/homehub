package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// WolTarget is one saved Wake-on-LAN machine in a user's address book.
type WolTarget struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	Name      string
	MAC       string
	Broadcast string
	Port      int
	CreatedAt time.Time
}

func (s *Store) ListWolTargets(ctx context.Context, userID uuid.UUID) ([]WolTarget, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, user_id, name, mac, broadcast, port, created_at
		 FROM wol_targets WHERE user_id=$1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WolTarget
	for rows.Next() {
		var t WolTarget
		if err := rows.Scan(&t.ID, &t.UserID, &t.Name, &t.MAC, &t.Broadcast, &t.Port, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) GetWolTarget(ctx context.Context, userID, id uuid.UUID) (*WolTarget, error) {
	var t WolTarget
	err := s.pool.QueryRow(ctx,
		`SELECT id, user_id, name, mac, broadcast, port, created_at
		 FROM wol_targets WHERE id=$1 AND user_id=$2`, id, userID).
		Scan(&t.ID, &t.UserID, &t.Name, &t.MAC, &t.Broadcast, &t.Port, &t.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *Store) CreateWolTarget(ctx context.Context, userID uuid.UUID, name, mac, broadcast string, port int) (*WolTarget, error) {
	var t WolTarget
	err := s.pool.QueryRow(ctx,
		`INSERT INTO wol_targets (user_id, name, mac, broadcast, port) VALUES ($1,$2,$3,$4,$5)
		 RETURNING id, user_id, name, mac, broadcast, port, created_at`,
		userID, name, mac, broadcast, port).
		Scan(&t.ID, &t.UserID, &t.Name, &t.MAC, &t.Broadcast, &t.Port, &t.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *Store) DeleteWolTarget(ctx context.Context, userID, id uuid.UUID) (bool, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM wol_targets WHERE id=$1 AND user_id=$2`, id, userID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}
