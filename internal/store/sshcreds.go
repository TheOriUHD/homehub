package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type SSHCredential struct {
	Resource string `db:"resource"`
	Host     string `db:"host"`
	Port     int    `db:"port"`
	Username string `db:"username"`
	AuthType string `db:"auth_type"`
	// HostID points at a central registry host; when set, the inline fields
	// above are ignored and the registry entry (plus its vault secret) is
	// used instead. Null keeps the legacy inline credential.
	HostID    *uuid.UUID `db:"host_id"`
	UpdatedAt time.Time  `db:"updated_at"`
}

func (s *Store) GetSSHCredential(ctx context.Context, resource string) (*SSHCredential, error) {
	return scanOne[SSHCredential](s.pool.Query(ctx, `
		select resource, host, port, username, auth_type, host_id, updated_at
		from ssh_credentials where resource = $1`, resource))
}

func (s *Store) UpsertSSHCredential(ctx context.Context, c *SSHCredential) error {
	_, err := s.pool.Exec(ctx, `
		insert into ssh_credentials (resource, host, port, username, auth_type, host_id)
		values ($1, $2, $3, $4, $5, $6)
		on conflict (resource) do update
		set host = excluded.host, port = excluded.port, username = excluded.username,
		    auth_type = excluded.auth_type, host_id = excluded.host_id, updated_at = now()`,
		c.Resource, c.Host, c.Port, c.Username, c.AuthType, c.HostID)
	return err
}

func (s *Store) DeleteSSHCredential(ctx context.Context, resource string) error {
	_, err := s.pool.Exec(ctx, `delete from ssh_credentials where resource = $1`, resource)
	return err
}

// DeleteSSHCredentialsUnder removes credentials for a resource subtree —
// used when an integration instance is deleted.
func (s *Store) DeleteSSHCredentialsUnder(ctx context.Context, prefix string) error {
	_, err := s.pool.Exec(ctx,
		`delete from ssh_credentials where resource = $1 or resource like $1 || '/%'`, prefix)
	return err
}
