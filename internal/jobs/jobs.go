// Package jobs runs background work: a PostgreSQL-backed queue with a
// worker pool, plus a cron scheduler that turns schedule rows into queued
// jobs. Everything is in-process — no external queue to operate.
package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/TheOriUHD/homehub/internal/store"
)

// HandlerFunc executes one job. A returned error triggers retry with backoff
// until the job's max attempts are exhausted.
type HandlerFunc func(ctx context.Context, payload json.RawMessage) error

type Engine struct {
	store   *store.Store
	logger  *slog.Logger
	workers int

	mu       sync.RWMutex
	handlers map[string]HandlerFunc

	wake    chan struct{}
	running atomic.Int32
	wg      sync.WaitGroup
}

func New(st *store.Store, logger *slog.Logger, workers int) *Engine {
	if workers <= 0 {
		workers = 4
	}
	return &Engine{
		store:    st,
		logger:   logger.With("component", "jobs"),
		workers:  workers,
		handlers: map[string]HandlerFunc{},
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

// Enqueue queues a job for immediate execution and wakes a worker.
func (e *Engine) Enqueue(ctx context.Context, kind string, payload any) (*store.Job, error) {
	job, err := e.store.EnqueueJob(ctx, kind, payload, time.Now(), 0, 3)
	if err != nil {
		return nil, err
	}
	e.Wake()
	return job, nil
}

// EnqueueAt schedules a job for a future run time.
func (e *Engine) EnqueueAt(ctx context.Context, kind string, payload any, runAt time.Time) (*store.Job, error) {
	return e.store.EnqueueJob(ctx, kind, payload, runAt, 0, 3)
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

	start := time.Now()
	err := func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("panic: %v\n%s", r, debug.Stack())
			}
		}()
		return handler(ctx, job.Payload)
	}()

	if err != nil {
		backoff := min(time.Duration(1<<job.Attempts)*15*time.Second, 10*time.Minute)
		logger.Warn("job failed", "kind", job.Kind, "job", job.ID, "attempt", job.Attempts, "error", err)
		if ferr := e.store.FailJob(ctx, job.ID, err.Error(), backoff); ferr != nil {
			logger.Error("record job failure", "job", job.ID, "error", ferr)
		}
		return
	}
	logger.Debug("job done", "kind", job.Kind, "job", job.ID, "duration", time.Since(start))
	if cerr := e.store.CompleteJob(ctx, job.ID); cerr != nil {
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
			if _, err := e.store.EnqueueJob(ctx, sched.Kind, sched.Payload, now, 0, 3); err != nil {
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
