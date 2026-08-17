package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type TOTPCredential struct {
	UserID           uuid.UUID  `db:"user_id"`
	SecretCiphertext []byte     `db:"secret_ciphertext"`
	ConfirmedAt      *time.Time `db:"confirmed_at"`
	CreatedAt        time.Time  `db:"created_at"`
}

// UpsertTOTP stores a new (unconfirmed) TOTP enrollment, replacing any
// previous unconfirmed attempt. A confirmed credential is never silently
// replaced — the service layer requires explicit disable first.
func (s *Store) UpsertTOTP(ctx context.Context, userID uuid.UUID, secretCiphertext []byte) error {
	_, err := s.pool.Exec(ctx, `
		insert into totp_credentials (user_id, secret_ciphertext)
		values ($1, $2)
		on conflict (user_id) do update
		set secret_ciphertext = excluded.secret_ciphertext, confirmed_at = null, created_at = now()
		where totp_credentials.confirmed_at is null`,
		userID, secretCiphertext)
	return err
}

func (s *Store) GetTOTP(ctx context.Context, userID uuid.UUID) (*TOTPCredential, error) {
	return scanOne[TOTPCredential](s.pool.Query(ctx,
		`select user_id, secret_ciphertext, confirmed_at, created_at from totp_credentials where user_id = $1`, userID))
}

func (s *Store) ConfirmTOTP(ctx context.Context, userID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		`update totp_credentials set confirmed_at = now() where user_id = $1 and confirmed_at is null`, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteTOTP(ctx context.Context, userID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `delete from totp_credentials where user_id = $1`, userID)
	return err
}

// ReplaceRecoveryCodes swaps the full set of recovery codes for a user.
func (s *Store) ReplaceRecoveryCodes(ctx context.Context, userID uuid.UUID, hashes [][]byte) error {
	return s.WithTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `delete from recovery_codes where user_id = $1`, userID); err != nil {
			return err
		}
		for _, h := range hashes {
			if _, err := tx.Exec(ctx,
				`insert into recovery_codes (user_id, code_hash) values ($1, $2)`, userID, h); err != nil {
				return err
			}
		}
		return nil
	})
}

// ConsumeRecoveryCode atomically marks a matching unused code as used.
// Returns false when no unused code matched.
func (s *Store) ConsumeRecoveryCode(ctx context.Context, userID uuid.UUID, hash []byte) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`update recovery_codes set used_at = now() where user_id = $1 and code_hash = $2 and used_at is null`,
		userID, hash)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (s *Store) CountUnusedRecoveryCodes(ctx context.Context, userID uuid.UUID) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`select count(*) from recovery_codes where user_id = $1 and used_at is null`, userID).Scan(&n)
	return n, err
}

func (s *Store) DeleteRecoveryCodes(ctx context.Context, userID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `delete from recovery_codes where user_id = $1`, userID)
	return err
}

type WebauthnCredential struct {
	ID              uuid.UUID  `db:"id"`
	UserID          uuid.UUID  `db:"user_id"`
	Name            string     `db:"name"`
	CredentialID    []byte     `db:"credential_id"`
	PublicKey       []byte     `db:"public_key"`
	AttestationType string     `db:"attestation_type"`
	Transports      []string   `db:"transports"`
	AAGUID          []byte     `db:"aaguid"`
	SignCount       int64      `db:"sign_count"`
	BackupEligible  bool       `db:"backup_eligible"`
	BackupState     bool       `db:"backup_state"`
	CreatedAt       time.Time  `db:"created_at"`
	LastUsedAt      *time.Time `db:"last_used_at"`
}

const webauthnCols = `id, user_id, name, credential_id, public_key, attestation_type,
	transports, aaguid, sign_count, backup_eligible, backup_state, created_at, last_used_at`

func (s *Store) CreateWebauthnCredential(ctx context.Context, c *WebauthnCredential) (*WebauthnCredential, error) {
	return scanOne[WebauthnCredential](s.pool.Query(ctx, `
		insert into webauthn_credentials
			(user_id, name, credential_id, public_key, attestation_type, transports, aaguid,
			 sign_count, backup_eligible, backup_state)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		returning `+webauthnCols,
		c.UserID, c.Name, c.CredentialID, c.PublicKey, c.AttestationType, c.Transports, c.AAGUID,
		c.SignCount, c.BackupEligible, c.BackupState))
}

func (s *Store) ListWebauthnCredentials(ctx context.Context, userID uuid.UUID) ([]WebauthnCredential, error) {
	return scanMany[WebauthnCredential](s.pool.Query(ctx,
		`select `+webauthnCols+` from webauthn_credentials where user_id = $1 order by created_at`, userID))
}

// WebauthnCredentialByCredID resolves a credential (and implicitly its user)
// from the authenticator's credential ID — used for discoverable/passkey login.
func (s *Store) WebauthnCredentialByCredID(ctx context.Context, credentialID []byte) (*WebauthnCredential, error) {
	return scanOne[WebauthnCredential](s.pool.Query(ctx,
		`select `+webauthnCols+` from webauthn_credentials where credential_id = $1`, credentialID))
}

func (s *Store) UpdateWebauthnUsage(ctx context.Context, id uuid.UUID, signCount int64, backupState bool) error {
	_, err := s.pool.Exec(ctx,
		`update webauthn_credentials set sign_count = $2, backup_state = $3, last_used_at = now() where id = $1`,
		id, signCount, backupState)
	return err
}

func (s *Store) RenameWebauthnCredential(ctx context.Context, userID, id uuid.UUID, name string) error {
	tag, err := s.pool.Exec(ctx,
		`update webauthn_credentials set name = $3 where id = $1 and user_id = $2`, id, userID, name)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteWebauthnCredential(ctx context.Context, userID, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		`delete from webauthn_credentials where id = $1 and user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
