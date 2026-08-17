package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type Job struct {
	ID          int64           `db:"id"`
	Kind        string          `db:"kind"`
	Payload     json.RawMessage `db:"payload"`
	Status      string          `db:"status"`
	Priority    int             `db:"priority"`
	RunAt       time.Time       `db:"run_at"`
	StartedAt   *time.Time      `db:"started_at"`
	FinishedAt  *time.Time      `db:"finished_at"`
	Attempts    int             `db:"attempts"`
	MaxAttempts int             `db:"max_attempts"`
	LastError   string          `db:"last_error"`
	CreatedAt   time.Time       `db:"created_at"`
}

const jobCols = `id, kind, payload, status, priority, run_at, started_at, finished_at,
	attempts, max_attempts, last_error, created_at`

func (s *Store) EnqueueJob(ctx context.Context, kind string, payload any, runAt time.Time, priority, maxAttempts int) (*Job, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return scanOne[Job](s.pool.Query(ctx, `
		insert into jobs (kind, payload, run_at, priority, max_attempts)
		values ($1, $2, $3, $4, $5)
		returning `+jobCols,
		kind, raw, runAt, priority, maxAttempts))
}

// ClaimJob atomically claims the highest-priority due job, if any. The
// SKIP LOCKED clause lets concurrent workers claim without contention.
func (s *Store) ClaimJob(ctx context.Context) (*Job, error) {
	job, err := scanOne[Job](s.pool.Query(ctx, `
		update jobs set status = 'running', started_at = now(), attempts = attempts + 1
		where id = (
			select id from jobs
			where status = 'pending' and run_at <= now()
			order by priority desc, run_at
			for update skip locked
			limit 1
		)
		returning `+jobCols))
	if err == ErrNotFound {
		return nil, nil
	}
	return job, err
}

func (s *Store) CompleteJob(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx,
		`update jobs set status = 'succeeded', finished_at = now(), last_error = '' where id = $1`, id)
	return err
}

// FailJob marks a failure; when attempts remain it reschedules with the given
// backoff, otherwise it finalizes as failed.
func (s *Store) FailJob(ctx context.Context, id int64, errMsg string, retryIn time.Duration) error {
	_, err := s.pool.Exec(ctx, `
		update jobs set
			status = case when attempts < max_attempts then 'pending' else 'failed' end,
			run_at = case when attempts < max_attempts then now() + $3::interval else run_at end,
			finished_at = case when attempts < max_attempts then null else now() end,
			last_error = $2
		where id = $1`,
		id, errMsg, retryIn.String())
	return err
}

type JobCounts struct {
	Pending      int `db:"pending"`
	Running      int `db:"running"`
	FailedRecent int `db:"failed_recent"`
}

func (s *Store) JobCounts(ctx context.Context) (*JobCounts, error) {
	var c JobCounts
	err := s.pool.QueryRow(ctx, `
		select
			count(*) filter (where status = 'pending'),
			count(*) filter (where status = 'running'),
			count(*) filter (where status = 'failed' and finished_at > now() - interval '24 hours')
		from jobs`).Scan(&c.Pending, &c.Running, &c.FailedRecent)
	return &c, err
}

// PruneJobs removes finished jobs past the retention window.
func (s *Store) PruneJobs(ctx context.Context, olderThan time.Duration) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		delete from jobs
		where status in ('succeeded', 'failed', 'cancelled') and finished_at < now() - $1::interval`,
		olderThan.String())
	return tag.RowsAffected(), err
}

type Schedule struct {
	ID        uuid.UUID       `db:"id"`
	Name      string          `db:"name"`
	Kind      string          `db:"kind"`
	Payload   json.RawMessage `db:"payload"`
	CronExpr  string          `db:"cron_expr"`
	Enabled   bool            `db:"enabled"`
	LastRunAt *time.Time      `db:"last_run_at"`
	NextRunAt *time.Time      `db:"next_run_at"`
	CreatedAt time.Time       `db:"created_at"`
	UpdatedAt time.Time       `db:"updated_at"`
}

const scheduleCols = `id, name, kind, payload, cron_expr, enabled, last_run_at, next_run_at, created_at, updated_at`

func (s *Store) ListSchedules(ctx context.Context) ([]Schedule, error) {
	return scanMany[Schedule](s.pool.Query(ctx,
		`select `+scheduleCols+` from schedules order by name`))
}

// EnsureSchedule creates a named system schedule when absent; an existing
// schedule (possibly retuned by an admin) is left untouched.
func (s *Store) EnsureSchedule(ctx context.Context, name, kind string, payload any, cronExpr string, nextRun time.Time) error {
	var exists bool
	if err := s.pool.QueryRow(ctx,
		`select exists (select 1 from schedules where name = $1)`, name).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		insert into schedules (name, kind, payload, cron_expr, enabled, next_run_at)
		values ($1, $2, $3, $4, true, $5)`,
		name, kind, raw, cronExpr, nextRun)
	return err
}

// DueSchedules claims schedules whose next_run_at has passed, advancing
// next_run_at to the provided computation via MarkScheduleRun.
func (s *Store) DueSchedules(ctx context.Context, now time.Time) ([]Schedule, error) {
	return scanMany[Schedule](s.pool.Query(ctx,
		`select `+scheduleCols+` from schedules where enabled and next_run_at is not null and next_run_at <= $1`, now))
}

func (s *Store) MarkScheduleRun(ctx context.Context, id uuid.UUID, ranAt, nextRun time.Time) error {
	_, err := s.pool.Exec(ctx,
		`update schedules set last_run_at = $2, next_run_at = $3, updated_at = now() where id = $1`,
		id, ranAt, nextRun)
	return err
}
