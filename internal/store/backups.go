package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type BackupTarget struct {
	ID            uuid.UUID       `db:"id"`
	Type          string          `db:"type"`
	Name          string          `db:"name"`
	Config        json.RawMessage `db:"config"`
	RetentionDays int             `db:"retention_days"`
	Encrypt       bool            `db:"encrypt"`
	Enabled       bool            `db:"enabled"`
	Status        string          `db:"status"`
	StatusMessage string          `db:"status_message"`
	CreatedAt     time.Time       `db:"created_at"`
	UpdatedAt     time.Time       `db:"updated_at"`
}

const backupTargetCols = `id, type, name, config, retention_days, encrypt, enabled, status, status_message, created_at, updated_at`

func (s *Store) ListBackupTargets(ctx context.Context) ([]BackupTarget, error) {
	return scanMany[BackupTarget](s.pool.Query(ctx,
		`select `+backupTargetCols+` from backup_targets order by lower(name)`))
}

func (s *Store) GetBackupTarget(ctx context.Context, id uuid.UUID) (*BackupTarget, error) {
	return scanOne[BackupTarget](s.pool.Query(ctx,
		`select `+backupTargetCols+` from backup_targets where id = $1`, id))
}

func (s *Store) CreateBackupTarget(ctx context.Context, typ, name string, config any, retentionDays int, encrypt bool) (*BackupTarget, error) {
	raw, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	return scanOne[BackupTarget](s.pool.Query(ctx, `
		insert into backup_targets (type, name, config, retention_days, encrypt)
		values ($1, $2, $3, $4, $5)
		returning `+backupTargetCols, typ, name, raw, retentionDays, encrypt))
}

func (s *Store) UpdateBackupTarget(ctx context.Context, id uuid.UUID, name string, config any, retentionDays int, encrypt, enabled bool) error {
	raw, err := json.Marshal(config)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `
		update backup_targets
		set name = $2, config = $3, retention_days = $4, encrypt = $5, enabled = $6, updated_at = now()
		where id = $1`, id, name, raw, retentionDays, encrypt, enabled)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) SetBackupTargetStatus(ctx context.Context, id uuid.UUID, status, message string) error {
	_, err := s.pool.Exec(ctx,
		`update backup_targets set status = $2, status_message = $3 where id = $1`, id, status, message)
	return err
}

func (s *Store) DeleteBackupTarget(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `delete from backup_targets where id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- runs ------------------------------------------------------------------

type BackupRun struct {
	ID         int64      `db:"id"`
	TargetID   *uuid.UUID `db:"target_id"`
	TargetName string     `db:"target_name"`
	Kind       string     `db:"kind"`
	Status     string     `db:"status"`
	FilePath   string     `db:"file_path"`
	SizeBytes  int64      `db:"size_bytes"`
	Error      string     `db:"error"`
	StartedAt  time.Time  `db:"started_at"`
	FinishedAt *time.Time `db:"finished_at"`
}

const backupRunCols = `id, target_id, target_name, kind, status, file_path, size_bytes, error, started_at, finished_at`

func (s *Store) StartBackupRun(ctx context.Context, targetID uuid.UUID, targetName, kind string) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		insert into backup_runs (target_id, target_name, kind) values ($1, $2, $3) returning id`,
		targetID, targetName, kind).Scan(&id)
	return id, err
}

func (s *Store) FinishBackupRun(ctx context.Context, id int64, status, filePath string, sizeBytes int64, errMsg string) error {
	_, err := s.pool.Exec(ctx, `
		update backup_runs
		set status = $2, file_path = $3, size_bytes = $4, error = $5, finished_at = now()
		where id = $1`, id, status, filePath, sizeBytes, errMsg)
	return err
}

// RecordBackupRun inserts an already-finished run. Used after a restore: the
// row created before pg_restore was wiped along with the rest of the database,
// so the outcome is recorded into the restored state instead.
func (s *Store) RecordBackupRun(ctx context.Context, targetID uuid.UUID, targetName, kind, status, filePath, errMsg string) error {
	_, err := s.pool.Exec(ctx, `
		insert into backup_runs (target_id, target_name, kind, status, file_path, error, finished_at)
		values ($1, $2, $3, $4, $5, $6, now())`,
		targetID, targetName, kind, status, filePath, errMsg)
	return err
}

// SweepStaleBackupRuns marks runs still 'running' as failed. Called at startup:
// anything running belongs to a previous process (crash, restart, or a snapshot
// of an in-flight backup replayed by a restore).
func (s *Store) SweepStaleBackupRuns(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		update backup_runs
		set status = 'failed', error = 'interrupted by restart', finished_at = now()
		where status = 'running'`)
	return tag.RowsAffected(), err
}

func (s *Store) ListBackupRuns(ctx context.Context, limit int) ([]BackupRun, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	return scanMany[BackupRun](s.pool.Query(ctx,
		`select `+backupRunCols+` from backup_runs order by started_at desc limit $1`, limit))
}
