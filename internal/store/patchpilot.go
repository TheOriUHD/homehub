package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// ---- hosts ------------------------------------------------------------------

type PatchpilotHost struct {
	ID             uuid.UUID  `db:"id"`
	IntegrationID  uuid.UUID  `db:"integration_id"`
	Name           string     `db:"name"`
	Hostname       string     `db:"hostname"`
	SSHPort        int        `db:"ssh_port"`
	SSHUser        string     `db:"ssh_user"`
	HostType       string     `db:"host_type"`
	PackageManager string     `db:"package_manager"`
	OSPretty       string     `db:"os_pretty"`
	Arch           string     `db:"arch"`
	IsProxmox      bool       `db:"is_proxmox"`
	Enabled        bool       `db:"enabled"`
	AutoReboot     bool       `db:"auto_reboot"`
	Notes          string     `db:"notes"`
	UpdatesPending int        `db:"updates_pending"`
	RebootRequired bool       `db:"reboot_required"`
	LastCheckedAt  *time.Time `db:"last_checked_at"`
	LastRunStatus  string     `db:"last_run_status"`
	LastRunAt      *time.Time `db:"last_run_at"`
	CreatedAt      time.Time  `db:"created_at"`
	UpdatedAt      time.Time  `db:"updated_at"`
}

const ppHostCols = `id, integration_id, name, hostname, ssh_port, ssh_user, host_type,
	package_manager, os_pretty, arch, is_proxmox, enabled, auto_reboot, notes,
	updates_pending, reboot_required, last_checked_at, last_run_status, last_run_at, created_at, updated_at`

func (s *Store) ListPatchpilotHosts(ctx context.Context, integrationID uuid.UUID) ([]PatchpilotHost, error) {
	return scanMany[PatchpilotHost](s.pool.Query(ctx,
		`select `+ppHostCols+` from patchpilot_hosts where integration_id = $1 order by name`, integrationID))
}

func (s *Store) GetPatchpilotHost(ctx context.Context, id uuid.UUID) (*PatchpilotHost, error) {
	return scanOne[PatchpilotHost](s.pool.Query(ctx,
		`select `+ppHostCols+` from patchpilot_hosts where id = $1`, id))
}

func (s *Store) CreatePatchpilotHost(ctx context.Context, h PatchpilotHost) (*PatchpilotHost, error) {
	return scanOne[PatchpilotHost](s.pool.Query(ctx, `
		insert into patchpilot_hosts
			(integration_id, name, hostname, ssh_port, ssh_user, host_type, package_manager,
			 os_pretty, arch, is_proxmox, enabled, auto_reboot, notes)
		values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		returning `+ppHostCols,
		h.IntegrationID, h.Name, h.Hostname, h.SSHPort, h.SSHUser, h.HostType, h.PackageManager,
		h.OSPretty, h.Arch, h.IsProxmox, h.Enabled, h.AutoReboot, h.Notes))
}

// UpdatePatchpilotHost writes the operator-mutable fields.
func (s *Store) UpdatePatchpilotHost(ctx context.Context, id uuid.UUID, name string, enabled, autoReboot bool, notes string) (*PatchpilotHost, error) {
	return scanOne[PatchpilotHost](s.pool.Query(ctx, `
		update patchpilot_hosts
		set name = $2, enabled = $3, auto_reboot = $4, notes = $5, updated_at = now()
		where id = $1
		returning `+ppHostCols, id, name, enabled, autoReboot, notes))
}

// SetPatchpilotHostSnapshot records the latest dry-run/run outcome for the grid.
func (s *Store) SetPatchpilotHostSnapshot(ctx context.Context, id uuid.UUID, pending int, rebootRequired bool, checkedAt time.Time) error {
	_, err := s.pool.Exec(ctx, `
		update patchpilot_hosts
		set updates_pending = $2, reboot_required = $3, last_checked_at = $4, updated_at = now()
		where id = $1`, id, pending, rebootRequired, checkedAt)
	return err
}

func (s *Store) SetPatchpilotHostRunStatus(ctx context.Context, id uuid.UUID, status string, at time.Time) error {
	_, err := s.pool.Exec(ctx, `
		update patchpilot_hosts set last_run_status = $2, last_run_at = $3, updated_at = now()
		where id = $1`, id, status, at)
	return err
}

func (s *Store) DeletePatchpilotHost(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `delete from patchpilot_hosts where id = $1`, id)
	return err
}

// ---- schedules --------------------------------------------------------------

type PatchpilotSchedule struct {
	ID             uuid.UUID  `db:"id"`
	IntegrationID  uuid.UUID  `db:"integration_id"`
	Name           string     `db:"name"`
	CronExpr       string     `db:"cron_expr"`
	Enabled        bool       `db:"enabled"`
	SuppressReboot bool       `db:"suppress_reboot"`
	LastRunAt      *time.Time `db:"last_run_at"`
	NextRunAt      *time.Time `db:"next_run_at"`
	LastSkippedAt  *time.Time `db:"last_skipped_at"`
	LastSkipReason string     `db:"last_skip_reason"`
	CreatedAt      time.Time  `db:"created_at"`
	UpdatedAt      time.Time  `db:"updated_at"`
}

const ppSchedCols = `id, integration_id, name, cron_expr, enabled, suppress_reboot,
	last_run_at, next_run_at, last_skipped_at, last_skip_reason, created_at, updated_at`

func (s *Store) ListPatchpilotSchedules(ctx context.Context, integrationID uuid.UUID) ([]PatchpilotSchedule, error) {
	return scanMany[PatchpilotSchedule](s.pool.Query(ctx,
		`select `+ppSchedCols+` from patchpilot_schedules where integration_id = $1 order by name`, integrationID))
}

func (s *Store) GetPatchpilotSchedule(ctx context.Context, id uuid.UUID) (*PatchpilotSchedule, error) {
	return scanOne[PatchpilotSchedule](s.pool.Query(ctx,
		`select `+ppSchedCols+` from patchpilot_schedules where id = $1`, id))
}

// DuePatchpilotSchedules returns enabled schedules whose next_run_at has passed.
func (s *Store) DuePatchpilotSchedules(ctx context.Context, now time.Time) ([]PatchpilotSchedule, error) {
	return scanMany[PatchpilotSchedule](s.pool.Query(ctx,
		`select `+ppSchedCols+` from patchpilot_schedules
		 where enabled and next_run_at is not null and next_run_at <= $1`, now))
}

func (s *Store) CreatePatchpilotSchedule(ctx context.Context, sc PatchpilotSchedule, hostIDs []uuid.UUID) (*PatchpilotSchedule, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	row, err := scanOne[PatchpilotSchedule](tx.Query(ctx, `
		insert into patchpilot_schedules (integration_id, name, cron_expr, enabled, suppress_reboot, next_run_at)
		values ($1,$2,$3,$4,$5,$6) returning `+ppSchedCols,
		sc.IntegrationID, sc.Name, sc.CronExpr, sc.Enabled, sc.SuppressReboot, sc.NextRunAt))
	if err != nil {
		return nil, err
	}
	for i, hid := range hostIDs {
		if _, err := tx.Exec(ctx,
			`insert into patchpilot_schedule_hosts (schedule_id, host_id, order_index) values ($1,$2,$3)`,
			row.ID, hid, i); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return row, nil
}

func (s *Store) UpdatePatchpilotSchedule(ctx context.Context, id uuid.UUID, sc PatchpilotSchedule, hostIDs []uuid.UUID) (*PatchpilotSchedule, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	row, err := scanOne[PatchpilotSchedule](tx.Query(ctx, `
		update patchpilot_schedules
		set name=$2, cron_expr=$3, enabled=$4, suppress_reboot=$5, next_run_at=$6, updated_at=now()
		where id=$1 returning `+ppSchedCols,
		id, sc.Name, sc.CronExpr, sc.Enabled, sc.SuppressReboot, sc.NextRunAt))
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `delete from patchpilot_schedule_hosts where schedule_id = $1`, id); err != nil {
		return nil, err
	}
	for i, hid := range hostIDs {
		if _, err := tx.Exec(ctx,
			`insert into patchpilot_schedule_hosts (schedule_id, host_id, order_index) values ($1,$2,$3)`,
			id, hid, i); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return row, nil
}

func (s *Store) MarkPatchpilotScheduleRun(ctx context.Context, id uuid.UUID, ranAt, nextRun time.Time) error {
	_, err := s.pool.Exec(ctx,
		`update patchpilot_schedules set last_run_at=$2, next_run_at=$3, updated_at=now() where id=$1`,
		id, ranAt, nextRun)
	return err
}

func (s *Store) SetPatchpilotScheduleNext(ctx context.Context, id uuid.UUID, next time.Time) error {
	_, err := s.pool.Exec(ctx, `update patchpilot_schedules set next_run_at=$2, updated_at=now() where id=$1`, id, next)
	return err
}

func (s *Store) MarkPatchpilotScheduleSkipped(ctx context.Context, id uuid.UUID, at time.Time, reason string) error {
	_, err := s.pool.Exec(ctx,
		`update patchpilot_schedules set last_skipped_at=$2, last_skip_reason=$3, updated_at=now() where id=$1`,
		id, at, reason)
	return err
}

func (s *Store) DeletePatchpilotSchedule(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `delete from patchpilot_schedules where id = $1`, id)
	return err
}

// ScheduleHostIDs returns the ordered host ids attached to a schedule.
func (s *Store) PatchpilotScheduleHostIDs(ctx context.Context, scheduleID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.pool.Query(ctx,
		`select host_id from patchpilot_schedule_hosts where schedule_id = $1 order by order_index`, scheduleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ---- runs -------------------------------------------------------------------

type PatchpilotRun struct {
	ID            uuid.UUID  `db:"id"`
	IntegrationID uuid.UUID  `db:"integration_id"`
	ScheduleID    *uuid.UUID `db:"schedule_id"`
	Trigger       string     `db:"trigger"`
	Status        string     `db:"status"`
	StartedAt     time.Time  `db:"started_at"`
	FinishedAt    *time.Time `db:"finished_at"`
	Summary       string     `db:"summary"`
}

const ppRunCols = `id, integration_id, schedule_id, trigger, status, started_at, finished_at, summary`

func (s *Store) CreatePatchpilotRun(ctx context.Context, r PatchpilotRun) (*PatchpilotRun, error) {
	return scanOne[PatchpilotRun](s.pool.Query(ctx, `
		insert into patchpilot_runs (integration_id, schedule_id, trigger, status)
		values ($1,$2,$3,$4) returning `+ppRunCols,
		r.IntegrationID, r.ScheduleID, r.Trigger, r.Status))
}

func (s *Store) GetPatchpilotRun(ctx context.Context, id uuid.UUID) (*PatchpilotRun, error) {
	return scanOne[PatchpilotRun](s.pool.Query(ctx, `select `+ppRunCols+` from patchpilot_runs where id = $1`, id))
}

func (s *Store) ListPatchpilotRuns(ctx context.Context, integrationID uuid.UUID, limit int) ([]PatchpilotRun, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	return scanMany[PatchpilotRun](s.pool.Query(ctx,
		`select `+ppRunCols+` from patchpilot_runs where integration_id=$1 order by started_at desc limit $2`,
		integrationID, limit))
}

// ActivePatchpilotRun returns the integration's currently running run, if any.
func (s *Store) ActivePatchpilotRun(ctx context.Context, integrationID uuid.UUID) (*PatchpilotRun, error) {
	return scanOne[PatchpilotRun](s.pool.Query(ctx,
		`select `+ppRunCols+` from patchpilot_runs where integration_id=$1 and status='running' order by started_at limit 1`,
		integrationID))
}

func (s *Store) FinishPatchpilotRun(ctx context.Context, id uuid.UUID, status, summary string) error {
	_, err := s.pool.Exec(ctx,
		`update patchpilot_runs set status=$2, summary=$3, finished_at=now() where id=$1`, id, status, summary)
	return err
}

// RecoverPatchpilotRuns flips any run left 'running' (and its host runs) to
// failed at startup — a crash mid-run must not block future runs forever.
func (s *Store) RecoverPatchpilotRuns(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, `
		update patchpilot_host_runs set status='failed', error_summary='interrupted by restart', finished_at=now()
		where status in ('pending','running')`); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `
		update patchpilot_runs set status='failed', summary='interrupted by restart', finished_at=now()
		where status='running'`)
	return err
}

// ---- host runs --------------------------------------------------------------

type PatchpilotHostRun struct {
	ID               uuid.UUID  `db:"id"`
	RunID            uuid.UUID  `db:"run_id"`
	HostID           *uuid.UUID `db:"host_id"`
	HostName         string     `db:"host_name"`
	Status           string     `db:"status"`
	ExitCode         *int       `db:"exit_code"`
	PackagesUpgraded int        `db:"packages_upgraded"`
	RebootRequired   bool       `db:"reboot_required"`
	ErrorSummary     string     `db:"error_summary"`
	Log              string     `db:"log"`
	StartedAt        *time.Time `db:"started_at"`
	FinishedAt       *time.Time `db:"finished_at"`
}

const ppHostRunCols = `id, run_id, host_id, host_name, status, exit_code, packages_upgraded,
	reboot_required, error_summary, log, started_at, finished_at`

func (s *Store) CreatePatchpilotHostRun(ctx context.Context, hr PatchpilotHostRun) (*PatchpilotHostRun, error) {
	return scanOne[PatchpilotHostRun](s.pool.Query(ctx, `
		insert into patchpilot_host_runs (run_id, host_id, host_name, status)
		values ($1,$2,$3,'pending') returning `+ppHostRunCols,
		hr.RunID, hr.HostID, hr.HostName))
}

func (s *Store) ListPatchpilotHostRuns(ctx context.Context, runID uuid.UUID) ([]PatchpilotHostRun, error) {
	return scanMany[PatchpilotHostRun](s.pool.Query(ctx,
		`select `+ppHostRunCols+` from patchpilot_host_runs where run_id=$1 order by started_at nulls last, host_name`, runID))
}

// RecentPatchpilotHostRuns lists a host's most recent slices across runs.
func (s *Store) RecentPatchpilotHostRuns(ctx context.Context, hostID uuid.UUID, limit int) ([]PatchpilotHostRun, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	return scanMany[PatchpilotHostRun](s.pool.Query(ctx,
		`select `+ppHostRunCols+` from patchpilot_host_runs where host_id=$1 order by started_at desc nulls last limit $2`,
		hostID, limit))
}

func (s *Store) StartPatchpilotHostRun(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `update patchpilot_host_runs set status='running', started_at=now() where id=$1`, id)
	return err
}

func (s *Store) AppendPatchpilotHostRunLog(ctx context.Context, id uuid.UUID, chunk string) error {
	_, err := s.pool.Exec(ctx, `update patchpilot_host_runs set log = log || $2 where id=$1`, id, chunk)
	return err
}

func (s *Store) FinishPatchpilotHostRun(ctx context.Context, id uuid.UUID, status string, exitCode *int, packages int, rebootRequired bool, errorSummary string) error {
	_, err := s.pool.Exec(ctx, `
		update patchpilot_host_runs
		set status=$2, exit_code=$3, packages_upgraded=$4, reboot_required=$5, error_summary=$6, finished_at=now()
		where id=$1`, id, status, exitCode, packages, rebootRequired, errorSummary)
	return err
}
