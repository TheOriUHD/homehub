package store

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
)

// GetHaDashboard loads one user's board for an instance; empty layout when
// none has been saved yet.
func (s *Store) GetHaDashboard(ctx context.Context, userID, integrationID uuid.UUID) (json.RawMessage, error) {
	var layout json.RawMessage
	err := s.pool.QueryRow(ctx,
		`select layout from ha_dashboards where user_id = $1 and integration_id = $2`,
		userID, integrationID).Scan(&layout)
	if err != nil {
		return json.RawMessage(`{"cards":[]}`), nil
	}
	return layout, nil
}

// PutHaDashboard saves (or replaces) one user's board for an instance.
func (s *Store) PutHaDashboard(ctx context.Context, userID, integrationID uuid.UUID, layout json.RawMessage) error {
	_, err := s.pool.Exec(ctx, `
		insert into ha_dashboards (user_id, integration_id, layout, updated_at)
		values ($1, $2, $3, now())
		on conflict (user_id, integration_id) do update set layout = $3, updated_at = now()`,
		userID, integrationID, layout)
	return err
}
