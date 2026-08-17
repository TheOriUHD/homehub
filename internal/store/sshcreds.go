package store

import (
	"context"
	"time"
)

type SSHCredential struct {
	Resource  string    `db:"resource"`
	Host      string    `db:"host"`
	Port      int       `db:"port"`
	Username  string    `db:"username"`
	AuthType  string    `db:"auth_type"`
	UpdatedAt time.Time `db:"updated_at"`
}

func (s *Store) GetSSHCredential(ctx context.Context, resource string) (*SSHCredential, error) {
	return scanOne[SSHCredential](s.pool.Query(ctx, `
		select resource, host, port, username, auth_type, updated_at
		from ssh_credentials where resource = $1`, resource))
}

func (s *Store) UpsertSSHCredential(ctx context.Context, c *SSHCredential) error {
	_, err := s.pool.Exec(ctx, `
		insert into ssh_credentials (resource, host, port, username, auth_type)
		values ($1, $2, $3, $4, $5)
		on conflict (resource) do update
		set host = excluded.host, port = excluded.port, username = excluded.username,
		    auth_type = excluded.auth_type, updated_at = now()`,
		c.Resource, c.Host, c.Port, c.Username, c.AuthType)
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
