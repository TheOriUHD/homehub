package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// GetSSHHostKey returns the pinned SHA256 fingerprint for host:port, or ""
// when nothing is pinned yet (trust-on-first-use has not happened).
func (s *Store) GetSSHHostKey(ctx context.Context, host string, port int) (string, error) {
	var fp string
	err := s.pool.QueryRow(ctx,
		`select fingerprint from ssh_known_hosts where host = $1 and port = $2`, host, port).Scan(&fp)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return fp, err
}

// PinSSHHostKey records the fingerprint for host:port on first contact. It does
// not overwrite an existing pin — a changed key must be handled deliberately
// (clearing the pin), never silently re-trusted.
func (s *Store) PinSSHHostKey(ctx context.Context, host string, port int, fingerprint string) error {
	_, err := s.pool.Exec(ctx, `
		insert into ssh_known_hosts (host, port, fingerprint)
		values ($1, $2, $3)
		on conflict (host, port) do nothing`, host, port, fingerprint)
	return err
}
