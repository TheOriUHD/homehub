package store

import (
	"context"
	"errors"
	"time"

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

// SSHKnownHost is one pinned key, as the operator sees it. The pin is keyed by
// address, not by whatever is using it: the SSH registry, a Docker daemon over
// SSH, a Minecraft host and a Proxmox guest console all share one entry, so
// re-trusting a rebuilt machine fixes every one of them at once.
type SSHKnownHost struct {
	Host        string    `db:"host"`
	Port        int       `db:"port"`
	Fingerprint string    `db:"fingerprint"`
	UpdatedAt   time.Time `db:"updated_at"`
}

// ListSSHKnownHosts returns every pin, address order.
func (s *Store) ListSSHKnownHosts(ctx context.Context) ([]SSHKnownHost, error) {
	return scanMany[SSHKnownHost](s.pool.Query(ctx,
		`select host, port, fingerprint, updated_at from ssh_known_hosts order by host, port`))
}

// TrustSSHHostKey replaces the pin for host:port. This is the deliberate act
// PinSSHHostKey refuses to do on its own: the caller has seen both
// fingerprints and decided the machine was rebuilt rather than impersonated.
func (s *Store) TrustSSHHostKey(ctx context.Context, host string, port int, fingerprint string) error {
	_, err := s.pool.Exec(ctx, `
		insert into ssh_known_hosts (host, port, fingerprint, updated_at)
		values ($1, $2, $3, now())
		on conflict (host, port) do update
		   set fingerprint = excluded.fingerprint, updated_at = now()`, host, port, fingerprint)
	return err
}

// ForgetSSHHostKey drops the pin, putting the address back to trust-on-first-
// use. Weaker than trusting a fingerprint you have looked at — whatever
// answers next is what gets pinned — so the UI offers it second.
func (s *Store) ForgetSSHHostKey(ctx context.Context, host string, port int) (bool, error) {
	tag, err := s.pool.Exec(ctx, `delete from ssh_known_hosts where host = $1 and port = $2`, host, port)
	return tag.RowsAffected() > 0, err
}
