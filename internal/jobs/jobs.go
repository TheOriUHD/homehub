// Package jobs runs background work: a PostgreSQL-backed queue with a
// worker pool, plus a cron scheduler that turns schedule rows into queued
// jobs. Everything is in-process — no external queue to operate.
package jobs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"

	"homehub/internal/store"
)

// Store is the slice of the database the engine needs; *store.Store
// satisfies it, and tests drive the engine with an in-memory one.
type Store interface {
	EnqueueJobDedup(ctx context.Context, kind string, payload any, runAt time.Time, priority, maxAttempts int, dedupKey string) (*store.Job, bool, error)
	ClaimJob(ctx context.Context) (*store.Job, error)
	CompleteJob(ctx context.Context, id int64) error
	FailJob(ctx context.Context, id int64, errMsg string, retryIn time.Duration) error
	FailJobFinal(ctx context.Context, id int64, errMsg string) error
	FailInterruptedJobs(ctx context.Context) (int64, error)
	JobCounts(ctx context.Context) (*store.JobCounts, error)
	DueSchedules(ctx context.Context, now time.Time) ([]store.Schedule, error)
	MarkScheduleRun(ctx context.Context, id uuid.UUID, ranAt, nextRun time.Time) error
}

// DefaultTimeout bounds a job whose kind sets no limit of its own. Long enough
// for any housekeeping pass; a job still going after an hour is stuck, and
// without a bound it would hold a worker (and its dedup slot) forever.
const DefaultTimeout = time.Hour

// KindOptions tunes one job kind.
type KindOptions struct {
	// Timeout is the per-run deadline; zero means DefaultTimeout. Handlers
	// see it as ctx's deadline, and a run that hits it is failed for good
	// rather than retried into the same wall.
	Timeout time.Duration
	// DedupKey names the target a payload acts on. While a job of this kind
	// for the same non-empty target is pending or running, Enqueue returns
	// that job instead of queueing another.
	DedupKey func(payload json.RawMessage) string
}

// TargetKey builds a DedupKey from the named top-level payload fields, so
// "the same target" ignores who asked (userId) and anything else incidental.
// With no fields every job of the kind shares one slot.
func TargetKey(fields ...string) func(json.RawMessage) string {
	return func(payload json.RawMessage) string {
		if len(fields) == 0 {
			return "*"
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal(payload, &m); err != nil {
			return ""
		}
		h := sha256.New()
		for _, f := range fields {
			h.Write([]byte(f))
			h.Write([]byte{0})
			h.Write(m[f])
			h.Write([]byte{0})
		}
		return hex.EncodeToString(h.Sum(nil))[:32]
	}
}

// HandlerFunc executes one job. A returned error triggers retry with backoff
// until the job's max attempts are exhausted.
type HandlerFunc func(ctx context.Context, payload json.RawMessage) error

type Engine struct {
	store   Store
	logger  *slog.Logger
	workers int

	mu       sync.RWMutex
	handlers map[string]HandlerFunc
	options  map[string]KindOptions

	wake    chan struct{}
	running atomic.Int32
	wg      sync.WaitGroup
}

func New(st Store, logger *slog.Logger, workers int) *Engine {
	if workers <= 0 {
		workers = 4
	}
	return &Engine{
		store:    st,
		logger:   logger.With("component", "jobs"),
		workers:  workers,
		handlers: map[string]HandlerFunc{},
		options:  map[string]KindOptions{},
		wake:     make(chan struct{}, 1),
	}
}

// Register binds a job kind to its handler. Kinds without handlers fail
// permanently when claimed, with a clear error in job history.
func (e *Engine) Register(kind string, fn HandlerFunc) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.handlers[kind] = fn
}

// Configure sets a kind's timeout and dedup policy. Call before Start.
func (e *Engine) Configure(kind string, opts KindOptions) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.options[kind] = opts
}

func (e *Engine) kindOptions(kind string) KindOptions {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.options[kind]
}

// dedupKey resolves the kind's target for a payload ("" = no dedup).
func (e *Engine) dedupKey(kind string, payload any) string {
	fn := e.kindOptions(kind).DedupKey
	if fn == nil {
		return ""
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	key := fn(raw)
	if key == "" {
		return ""
	}
	return kind + "|" + key
}

func (e *Engine) enqueue(ctx context.Context, kind string, payload any, runAt time.Time, maxAttempts int, dedupKey string) (*store.Job, error) {
	job, _, err := e.store.EnqueueJobDedup(ctx, kind, payload, runAt, 0, maxAttempts, dedupKey)
	return job, err
}

// Enqueue queues a job for immediate execution and wakes a worker. For a kind
// with a DedupKey, an already pending or running job for the same target is
// returned instead of queueing a duplicate.
func (e *Engine) Enqueue(ctx context.Context, kind string, payload any) (*store.Job, error) {
	job, err := e.enqueue(ctx, kind, payload, time.Now(), 3, e.dedupKey(kind, payload))
	if err != nil {
		return nil, err
	}
	e.Wake()
	return job, nil
}

// EnqueueAt schedules a job for a future run time.
func (e *Engine) EnqueueAt(ctx context.Context, kind string, payload any, runAt time.Time) (*store.Job, error) {
	return e.enqueue(ctx, kind, payload, runAt, 3, e.dedupKey(kind, payload))
}

// EnqueueOnce queues a job that is never retried (max attempts 1). Suited to
// deterministic work like conversions, where a retry would only repeat the
// same failure.
func (e *Engine) EnqueueOnce(ctx context.Context, kind string, payload any) (*store.Job, error) {
	job, err := e.enqueue(ctx, kind, payload, time.Now(), 1, e.dedupKey(kind, payload))
	if err != nil {
		return nil, err
	}
	e.Wake()
	return job, nil
}

// RecoverInterrupted fails every job a previous process left running. Call
// once at boot, before Start: nothing in this process has claimed a job yet,
// so anything marked running belongs to a worker that no longer exists.
func (e *Engine) RecoverInterrupted(ctx context.Context) (int64, error) {
	return e.store.FailInterruptedJobs(ctx)
}

// Wake nudges the worker pool without blocking.
func (e *Engine) Wake() {
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

// Start launches workers and the schedule loop; they exit when ctx ends.
func (e *Engine) Start(ctx context.Context) {
	for i := range e.workers {
		e.wg.Add(1)
		go e.worker(ctx, i)
	}
	e.wg.Add(1)
	go e.scheduleLoop(ctx)
}

// Wait blocks until all workers have exited (after ctx cancellation).
func (e *Engine) Wait() { e.wg.Wait() }

// Health reports queue depth and worker activity for the health page.
type Health struct {
	Workers      int
	ActiveJobs   int
	Pending      int
	Running      int
	FailedRecent int
}

func (e *Engine) Health(ctx context.Context) (*Health, error) {
	counts, err := e.store.JobCounts(ctx)
	if err != nil {
		return nil, err
	}
	return &Health{
		Workers:      e.workers,
		ActiveJobs:   int(e.running.Load()),
		Pending:      counts.Pending,
		Running:      counts.Running,
		FailedRecent: counts.FailedRecent,
	}, nil
}

func (e *Engine) worker(ctx context.Context, id int) {
	defer e.wg.Done()
	logger := e.logger.With("worker", id)
	for {
		job, err := e.store.ClaimJob(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			logger.Error("claim failed", "error", err)
		}
		if job != nil {
			e.run(ctx, logger, job)
			continue // drain the queue before sleeping
		}
		select {
		case <-ctx.Done():
			return
		case <-e.wake:
		case <-time.After(2 * time.Second):
		}
	}
}

func (e *Engine) run(ctx context.Context, logger *slog.Logger, job *store.Job) {
	e.running.Add(1)
	defer e.running.Add(-1)

	e.mu.RLock()
	handler, ok := e.handlers[job.Kind]
	e.mu.RUnlock()

	if !ok {
		logger.Error("no handler for job kind", "kind", job.Kind, "job", job.ID)
		_ = e.store.FailJob(ctx, job.ID, fmt.Sprintf("no handler registered for kind %q", job.Kind), 0)
		return
	}

	timeout := e.kindOptions(job.Kind).Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	jobCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	err := func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("panic: %v\n%s", r, debug.Stack())
			}
		}()
		return handler(jobCtx, job.Payload)
	}()

	// The outcome is written even when shutdown cancelled ctx mid-job: a
	// handler that finished deserves its "succeeded", and one that was cut
	// short must not be left "running" for the next boot to guess about.
	recordCtx, cancelRecord := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancelRecord()

	if err != nil {
		if errors.Is(jobCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
			msg := fmt.Sprintf("timed out after %s: %v", timeout, err)
			logger.Warn("job timed out", "kind", job.Kind, "job", job.ID, "timeout", timeout, "error", err)
			if ferr := e.store.FailJobFinal(recordCtx, job.ID, msg); ferr != nil {
				logger.Error("record job timeout", "job", job.ID, "error", ferr)
			}
			return
		}
		backoff := min(time.Duration(1<<job.Attempts)*15*time.Second, 10*time.Minute)
		logger.Warn("job failed", "kind", job.Kind, "job", job.ID, "attempt", job.Attempts, "error", err)
		if ferr := e.store.FailJob(recordCtx, job.ID, err.Error(), backoff); ferr != nil {
			logger.Error("record job failure", "job", job.ID, "error", ferr)
		}
		return
	}
	logger.Debug("job done", "kind", job.Kind, "job", job.ID, "duration", time.Since(start))
	if cerr := e.store.CompleteJob(recordCtx, job.ID); cerr != nil {
		logger.Error("record job completion", "job", job.ID, "error", cerr)
	}
}

// scheduleLoop enqueues jobs for due schedule rows and advances their next
// run using standard 5-field cron expressions.
func (e *Engine) scheduleLoop(ctx context.Context) {
	defer e.wg.Done()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		now := time.Now()
		due, err := e.store.DueSchedules(ctx, now)
		if err != nil {
			if ctx.Err() == nil {
				e.logger.Error("load due schedules", "error", err)
			}
			continue
		}
		for _, sched := range due {
			next, err := NextRun(sched.CronExpr, now)
			if err != nil {
				e.logger.Error("invalid cron expression on schedule", "schedule", sched.Name, "expr", sched.CronExpr, "error", err)
				continue
			}
			// One live job per schedule: a tick that lands while the previous
			// run is still queued or going joins it instead of stacking up
			// behind a slow handler.
			if _, _, err := e.store.EnqueueJobDedup(ctx, sched.Kind, sched.Payload, now, 0, 3, "schedule|"+sched.ID.String()); err != nil {
				e.logger.Error("enqueue scheduled job", "schedule", sched.Name, "error", err)
				continue
			}
			if err := e.store.MarkScheduleRun(ctx, sched.ID, now, next); err != nil {
				e.logger.Error("advance schedule", "schedule", sched.Name, "error", err)
			}
			e.Wake()
		}
	}
}

// NextRun computes the next fire time for a standard cron expression.
func NextRun(expr string, after time.Time) (time.Time, error) {
	sched, err := cron.ParseStandard(expr)
	if err != nil {
		return time.Time{}, err
	}
	return sched.Next(after), nil
}
