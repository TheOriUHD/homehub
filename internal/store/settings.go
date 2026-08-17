package store

import (
	"context"
	"encoding/json"
)

// GetSetting returns the raw JSON value for a key, or ErrNotFound.
func (s *Store) GetSetting(ctx context.Context, key string) (json.RawMessage, error) {
	var raw json.RawMessage
	err := s.pool.QueryRow(ctx, `select value from settings where key = $1`, key).Scan(&raw)
	if err != nil {
		return nil, notFound(err)
	}
	return raw, nil
}

func (s *Store) SetSetting(ctx context.Context, key string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		insert into settings (key, value) values ($1, $2)
		on conflict (key) do update set value = excluded.value, updated_at = now()`,
		key, raw)
	return err
}

// GetSettingInto unmarshals a setting into dst; returns false when unset.
func (s *Store) GetSettingInto(ctx context.Context, key string, dst any) (bool, error) {
	raw, err := s.GetSetting(ctx, key)
	if err == ErrNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, json.Unmarshal(raw, dst)
}

// AllSettings returns every settings row as raw JSON keyed by name.
func (s *Store) AllSettings(ctx context.Context) (map[string]json.RawMessage, error) {
	rows, err := s.pool.Query(ctx, `select key, value from settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]json.RawMessage{}
	for rows.Next() {
		var k string
		var v json.RawMessage
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}
