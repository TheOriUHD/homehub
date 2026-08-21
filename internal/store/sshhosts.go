package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// SSHHost is one entry of the central SSH registry. The secret (password or
// private key) lives in the vault under scope "sshhost" keyed by ID.
type SSHHost struct {
	ID        uuid.UUID `db:"id"`
	Name      string    `db:"name"`
	Hostname  string    `db:"hostname"`
	Port      int       `db:"port"`
	Username  string    `db:"username"`
	AuthType  string    `db:"auth_type"`
	Notes     string    `db:"notes"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

const sshHostCols = `id, name, hostname, port, username, auth_type, notes, created_at, updated_at`

func (s *Store) ListSSHHosts(ctx context.Context) ([]SSHHost, error) {
	return scanMany[SSHHost](s.pool.Query(ctx,
		`select `+sshHostCols+` from ssh_hosts order by name asc`))
}

func (s *Store) GetSSHHost(ctx context.Context, id uuid.UUID) (*SSHHost, error) {
	return scanOne[SSHHost](s.pool.Query(ctx,
		`select `+sshHostCols+` from ssh_hosts where id = $1`, id))
}

func (s *Store) GetSSHHostByName(ctx context.Context, name string) (*SSHHost, error) {
	return scanOne[SSHHost](s.pool.Query(ctx,
		`select `+sshHostCols+` from ssh_hosts where lower(name) = lower($1)`, name))
}

func (s *Store) CreateSSHHost(ctx context.Context, h *SSHHost) (*SSHHost, error) {
	return scanOne[SSHHost](s.pool.Query(ctx, `
		insert into ssh_hosts (name, hostname, port, username, auth_type, notes)
		values ($1, $2, $3, $4, $5, $6)
		returning `+sshHostCols,
		h.Name, h.Hostname, h.Port, h.Username, h.AuthType, h.Notes))
}

func (s *Store) UpdateSSHHost(ctx context.Context, h *SSHHost) (*SSHHost, error) {
	return scanOne[SSHHost](s.pool.Query(ctx, `
		update ssh_hosts set name = $2, hostname = $3, port = $4, username = $5,
			auth_type = $6, notes = $7, updated_at = now()
		where id = $1
		returning `+sshHostCols,
		h.ID, h.Name, h.Hostname, h.Port, h.Username, h.AuthType, h.Notes))
}

func (s *Store) DeleteSSHHost(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `delete from ssh_hosts where id = $1`, id)
	return err
}

// CountSSHHostReferences reports how many per-resource credentials point at
// a registry host — shown before deletion so the impact is never a surprise.
func (s *Store) CountSSHHostReferences(ctx context.Context, id uuid.UUID) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`select count(*) from ssh_credentials where host_id = $1`, id).Scan(&n)
	return n, err
}

// ListResourcesForSSHHost names the resources bound to a host.
func (s *Store) ListResourcesForSSHHost(ctx context.Context, id uuid.UUID) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`select resource from ssh_credentials where host_id = $1 order by resource`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
