package store

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"
)

// MinecraftServer is the companion-reported metadata for one instance.
type MinecraftServer struct {
	IntegrationID    uuid.UUID  `db:"integration_id"`
	ServerName       string     `db:"server_name"`
	Loader           string     `db:"loader"`
	McVersion        string     `db:"mc_version"`
	CompanionVersion string     `db:"companion_version"`
	PairedAt         *time.Time `db:"paired_at"`
	LastConnectedAt  *time.Time `db:"last_connected_at"`
	LastRemoteIP     string     `db:"last_remote_ip"`
}

const mcServerCols = `integration_id, server_name, loader, mc_version, companion_version, paired_at, last_connected_at, last_remote_ip`

// CreateMinecraftPairing issues a fresh 6-digit code for an instance,
// replacing any outstanding codes for it.
func (s *Store) CreateMinecraftPairing(ctx context.Context, integrationID uuid.UUID, ttl time.Duration) (string, time.Time, error) {
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
		if _, err := tx.Exec(ctx, `insert into minecraft_pairings (code, integration_id, expires_at) values ($1, $2, $3)`,
			code, integrationID, expires); err != nil {
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
// bound integration id, or ErrNotFound for unknown/expired codes.
func (s *Store) ConsumeMinecraftPairing(ctx context.Context, code string) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		delete from minecraft_pairings
		where code = $1 and expires_at > now()
		returning integration_id`, code).Scan(&id)
	if err != nil {
		return uuid.Nil, ErrNotFound
	}
	return id, nil
}

// PendingMinecraftPairing reports an unexpired code for the instance, if any.
func (s *Store) PendingMinecraftPairing(ctx context.Context, integrationID uuid.UUID) (string, *time.Time, error) {
	var code string
	var expires time.Time
	err := s.pool.QueryRow(ctx, `
		select code, expires_at from minecraft_pairings
		where integration_id = $1 and expires_at > now()`, integrationID).Scan(&code, &expires)
	if err != nil {
		return "", nil, nil // no pending code is not an error
	}
	return code, &expires, nil
}

// UpsertMinecraftServer records companion metadata at pair/connect time.
func (s *Store) UpsertMinecraftServer(ctx context.Context, integrationID uuid.UUID,
	serverName, loader, mcVersion, companionVersion, remoteIP string, paired bool) error {
	pairedAt := "paired_at"
	if paired {
		pairedAt = "now()"
	}
	_, err := s.pool.Exec(ctx, fmt.Sprintf(`
		insert into minecraft_servers (integration_id, server_name, loader, mc_version, companion_version, paired_at, last_connected_at, last_remote_ip)
		values ($1, $2, $3, $4, $5, now(), now(), $6)
		on conflict (integration_id) do update set
			server_name = $2, loader = $3, mc_version = $4, companion_version = $5,
			paired_at = %s, last_connected_at = now(), last_remote_ip = $6`, pairedAt),
		integrationID, serverName, loader, mcVersion, companionVersion, remoteIP)
	return err
}

// GetMinecraftServer loads companion metadata; ErrNotFound when never paired.
func (s *Store) GetMinecraftServer(ctx context.Context, integrationID uuid.UUID) (*MinecraftServer, error) {
	return scanOne[MinecraftServer](s.pool.Query(ctx,
		`select `+mcServerCols+` from minecraft_servers where integration_id = $1`, integrationID))
}
