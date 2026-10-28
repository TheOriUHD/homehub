package store

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"
)

// MinecraftServer is one server within a Minecraft network integration —
// a proxy or a subserver, bound to its own companion. For instances that
// predate networks, ID equals IntegrationID (the wrap migration), which
// keeps the tokens those companions already hold valid.
type MinecraftServer struct {
	ID               uuid.UUID  `db:"id"`
	IntegrationID    uuid.UUID  `db:"integration_id"`
	Label            string     `db:"label"`
	ServerName       string     `db:"server_name"`
	Loader           string     `db:"loader"`
	McVersion        string     `db:"mc_version"`
	CompanionVersion string     `db:"companion_version"`
	PairedAt         *time.Time `db:"paired_at"`
	LastConnectedAt  *time.Time `db:"last_connected_at"`
	LastRemoteIP     string     `db:"last_remote_ip"`
	CreatedAt        time.Time  `db:"created_at"`
}

const mcServerCols = `id, integration_id, label, server_name, loader, mc_version, companion_version, paired_at, last_connected_at, last_remote_ip, created_at`

// CreateMinecraftPairing issues a fresh 6-digit code for a network,
// replacing any outstanding codes for it. The label names the server the
// code will create.
func (s *Store) CreateMinecraftPairing(ctx context.Context, integrationID uuid.UUID, label string, ttl time.Duration) (string, time.Time, error) {
	expires := time.Now().Add(ttl)
	// A few retries absorb the (tiny) collision chance on the 6-digit space.
	for attempt := 0; attempt < 5; attempt++ {
		n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
		if err != nil {
			return "", time.Time{}, err
		}
		code := fmt.Sprintf("%06d", n.Int64())
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return "", time.Time{}, err
		}
		if _, err := tx.Exec(ctx, `delete from minecraft_pairings where integration_id = $1`, integrationID); err != nil {
			_ = tx.Rollback(ctx)
			return "", time.Time{}, err
		}
		if _, err := tx.Exec(ctx, `insert into minecraft_pairings (code, integration_id, label, expires_at) values ($1, $2, $3, $4)`,
			code, integrationID, label, expires); err != nil {
			_ = tx.Rollback(ctx)
			if IsUniqueViolation(err) {
				continue
			}
			return "", time.Time{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return "", time.Time{}, err
		}
		return code, expires, nil
	}
	return "", time.Time{}, fmt.Errorf("could not allocate a pairing code")
}

// ConsumeMinecraftPairing redeems a live code, deleting it. Returns the
// bound network id and the label chosen for the new server.
func (s *Store) ConsumeMinecraftPairing(ctx context.Context, code string) (uuid.UUID, string, error) {
	var id uuid.UUID
	var label string
	err := s.pool.QueryRow(ctx, `
		delete from minecraft_pairings
		where code = $1 and expires_at > now()
		returning integration_id, label`, code).Scan(&id, &label)
	if err != nil {
		return uuid.Nil, "", ErrNotFound
	}
	return id, label, nil
}

// RecordFailedMinecraftPairing counts one wrong code against every live
// code and withdraws those that have now seen maxFailures, returning how
// many were withdrawn. The guess could have been aimed at any of them, so
// each one's budget shrinks.
func (s *Store) RecordFailedMinecraftPairing(ctx context.Context, maxFailures int) (int64, error) {
	if _, err := s.pool.Exec(ctx, `
		update minecraft_pairings set failed_attempts = failed_attempts + 1
		where expires_at > now()`); err != nil {
		return 0, err
	}
	tag, err := s.pool.Exec(ctx,
		`delete from minecraft_pairings where failed_attempts >= $1`, maxFailures)
	return tag.RowsAffected(), err
}

// PendingMinecraftPairing reports an unexpired code for the network, if any.
func (s *Store) PendingMinecraftPairing(ctx context.Context, integrationID uuid.UUID) (string, string, *time.Time, error) {
	var code, label string
	var expires time.Time
	err := s.pool.QueryRow(ctx, `
		select code, label, expires_at from minecraft_pairings
		where integration_id = $1 and expires_at > now()`, integrationID).Scan(&code, &label, &expires)
	if err != nil {
		return "", "", nil, nil // no pending code is not an error
	}
	return code, label, &expires, nil
}

// CreateMinecraftServer adds a server row at pairing time.
func (s *Store) CreateMinecraftServer(ctx context.Context, integrationID uuid.UUID, label string) (*MinecraftServer, error) {
	return scanOne[MinecraftServer](s.pool.Query(ctx, `
		insert into minecraft_network_servers (integration_id, label, paired_at)
		values ($1, $2, now())
		returning `+mcServerCols, integrationID, label))
}

// TouchMinecraftServer records companion-reported metadata on connect.
func (s *Store) TouchMinecraftServer(ctx context.Context, serverID uuid.UUID,
	serverName, loader, mcVersion, companionVersion, remoteIP string) error {
	_, err := s.pool.Exec(ctx, `
		update minecraft_network_servers set
			server_name = $2, loader = $3, mc_version = $4, companion_version = $5,
			last_connected_at = now(), last_remote_ip = $6
		where id = $1`,
		serverID, serverName, loader, mcVersion, companionVersion, remoteIP)
	return err
}

// GetMinecraftServer loads one server row; ErrNotFound when it was removed.
func (s *Store) GetMinecraftServer(ctx context.Context, serverID uuid.UUID) (*MinecraftServer, error) {
	return scanOne[MinecraftServer](s.pool.Query(ctx,
		`select `+mcServerCols+` from minecraft_network_servers where id = $1`, serverID))
}

// ListMinecraftServers returns a network's servers, oldest first.
func (s *Store) ListMinecraftServers(ctx context.Context, integrationID uuid.UUID) ([]MinecraftServer, error) {
	return scanMany[MinecraftServer](s.pool.Query(ctx,
		`select `+mcServerCols+` from minecraft_network_servers where integration_id = $1 order by created_at`, integrationID))
}

// RenameMinecraftServer updates a server's label.
func (s *Store) RenameMinecraftServer(ctx context.Context, serverID uuid.UUID, label string) error {
	_, err := s.pool.Exec(ctx,
		`update minecraft_network_servers set label = $2 where id = $1`, serverID, label)
	return err
}

// MoveMinecraftServer reassigns a server to another network.
func (s *Store) MoveMinecraftServer(ctx context.Context, serverID, targetIntegrationID uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`update minecraft_network_servers set integration_id = $2 where id = $1`, serverID, targetIntegrationID)
	return err
}

// DeleteMinecraftServer removes a server from its network. The companion's
// token dies with the row — hello validates the row exists.
func (s *Store) DeleteMinecraftServer(ctx context.Context, integrationID, serverID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		`delete from minecraft_network_servers where id = $1 and integration_id = $2`, serverID, integrationID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
