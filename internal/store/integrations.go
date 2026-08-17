package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type Integration struct {
	ID              uuid.UUID       `db:"id"`
	Type            string          `db:"type"`
	Name            string          `db:"name"`
	Config          json.RawMessage `db:"config"`
	Enabled         bool            `db:"enabled"`
	Status          string          `db:"status"`
	StatusMessage   string          `db:"status_message"`
	StatusChangedAt time.Time       `db:"status_changed_at"`
	CreatedAt       time.Time       `db:"created_at"`
	UpdatedAt       time.Time       `db:"updated_at"`
}

const integrationCols = `id, type, name, config, enabled, status, status_message, status_changed_at, created_at, updated_at`

func (s *Store) CreateIntegration(ctx context.Context, typ, name string, config any) (*Integration, error) {
	raw, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	return scanOne[Integration](s.pool.Query(ctx, `
		insert into integrations (type, name, config) values ($1, $2, $3)
		returning `+integrationCols, typ, name, raw))
}

func (s *Store) GetIntegration(ctx context.Context, id uuid.UUID) (*Integration, error) {
	return scanOne[Integration](s.pool.Query(ctx,
		`select `+integrationCols+` from integrations where id = $1`, id))
}

func (s *Store) ListIntegrations(ctx context.Context) ([]Integration, error) {
	return scanMany[Integration](s.pool.Query(ctx,
		`select `+integrationCols+` from integrations order by lower(name)`))
}

func (s *Store) UpdateIntegration(ctx context.Context, id uuid.UUID, name string, config any, enabled bool) error {
	raw, err := json.Marshal(config)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `
		update integrations set name = $2, config = $3, enabled = $4, updated_at = now()
		where id = $1`, id, name, raw, enabled)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetIntegrationStatus records a health transition; returns true when the
// status actually changed (callers use this to publish/notify only on edges).
func (s *Store) SetIntegrationStatus(ctx context.Context, id uuid.UUID, status, message string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		update integrations
		set status = $2, status_message = $3, status_changed_at = now()
		where id = $1 and (status <> $2 or status_message <> $3)`,
		id, status, message)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (s *Store) DeleteIntegration(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `delete from integrations where id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
