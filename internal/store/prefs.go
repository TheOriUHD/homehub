package store

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
)

// GetUserPref returns the stored document for one (user, key), or
// ErrNotFound when the user never saved that surface.
func (s *Store) GetUserPref(ctx context.Context, userID uuid.UUID, key string) (json.RawMessage, error) {
	var value json.RawMessage
	err := s.pool.QueryRow(ctx,
		`select value from user_prefs where user_id = $1 and key = $2`,
		userID, key).Scan(&value)
	if err != nil {
		return nil, notFound(err)
	}
	return value, nil
}

// SetUserPref upserts one (user, key) document.
func (s *Store) SetUserPref(ctx context.Context, userID uuid.UUID, key string, value json.RawMessage) error {
	_, err := s.pool.Exec(ctx, `
		insert into user_prefs (user_id, key, value) values ($1, $2, $3)
		on conflict (user_id, key) do update set value = excluded.value, updated_at = now()`,
		userID, key, value)
	return err
}
