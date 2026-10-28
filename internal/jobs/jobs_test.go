package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"homehub/internal/store"
)

// memStore mimics the jobs table closely enough to exercise the engine: the
// partial unique index on dedup_key is modelled as "no two active rows share
// a key".
type memStore struct {
	mu     sync.Mutex
	nextID int64
	jobs   map[int64]*store.Job
	keys   map[int64]string
}

func newMemStore() *memStore {
	return &memStore{jobs: map[int64]*store.Job{}, keys: map[int64]string{}}
}

func active(j *store.Job) bool { return j.Status == "pending" || j.Status == "running" }

func (m *memStore) EnqueueJobDedup(_ context.Context, kind string, payload any, runAt time.Time, priority, maxAttempts int, dedupKey string) (*store.Job, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if dedupKey != "" {
		for id, k := range m.keys {
			if k == dedupKey && active(m.jobs[id]) {
				return m.jobs[id], false, nil
			}
		}
	}
	raw, _ := json.Marshal(payload)
	m.nextID++
	j := &store.Job{ID: m.nextID, Kind: kind, Payload: raw, Status: "pending", RunAt: runAt, MaxAttempts: maxAttempts}
	m.jobs[j.ID] = j
	m.keys[j.ID] = dedupKey
	return j, true, nil
}

func (m *memStore) ClaimJob(context.Context) (*store.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id := int64(1); id <= m.nextID; id++ {
		if j := m.jobs[id]; j != nil && j.Status == "pending" && !j.RunAt.After(time.Now()) {
			j.Status = "running"
			j.Attempts++
			c := *j
			return &c, nil
		}
	}
	return nil, nil
}

func (m *memStore) set(id int64, status, msg string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.jobs[id].Status = status
	m.jobs[id].LastError = msg
}

func (m *memStore) CompleteJob(_ context.Context, id int64) error {
	m.set(id, "succeeded", "")
	return nil
}

func (m *memStore) FailJob(_ context.Context, id int64, msg string, retry time.Duration) error {
	m.mu.Lock()
	j := m.jobs[id]
	m.mu.Unlock()
	if j.Attempts < j.MaxAttempts {
		m.set(id, "pending", msg)
		m.mu.Lock()
		j.RunAt = time.Now().Add(retry)
		m.mu.Unlock()
		return nil
	}
	m.set(id, "failed", msg)
	return nil
}

func (m *memStore) FailJobFinal(_ context.Context, id int64, msg string) error {
	m.set(id, "failed", msg)
	return nil
}

func (m *memStore) FailInterruptedJobs(context.Context) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int64
	for _, j := range m.jobs {
		if j.Status == "running" {
			j.Status, j.LastError = "failed", "interrupted by restart"
			n++
		}
	}
	return n, nil
}

func (m *memStore) JobCounts(context.Context) (*store.JobCounts, error) { return &store.JobCounts{}, nil }
func (m *memStore) DueSchedules(context.Context, time.Time) ([]store.Schedule, error) {
	return nil, nil
}
func (m *memStore) MarkScheduleRun(context.Context, uuid.UUID, time.Time, time.Time) error {
	return nil
}

func (m *memStore) status(id int64) (string, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.jobs[id].Status, m.jobs[id].LastError
}

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A restart leaves the previous process's running rows behind; boot must
// close them out instead of letting them read as running forever.
func TestRecoverInterruptedFailsOrphanedRunningJobs(t *testing.T) {
	ms := newMemStore()
	e := New(ms, quietLogger(), 1)
	ctx := context.Background()
	stuck, _ := e.Enqueue(ctx, "backup.app", map[string]any{})
	queued, _ := e.Enqueue(ctx, "backup.app", map[string]any{})
	ms.set(stuck.ID, "running", "")

	n, err := e.RecoverInterrupted(ctx)
	if err != nil || n != 1 {
		t.Fatalf("recovered %d, %v; want 1", n, err)
	}
	if st, msg := ms.status(stuck.ID); st != "failed" || msg != "interrupted by restart" {
		t.Fatalf("stuck job is %s (%q)", st, msg)
	}
	// Queued work has not started, so it is still valid to run.
	if st, _ := ms.status(queued.ID); st != "pending" {
		t.Fatalf("queued job is %s, want pending", st)
	}
}

// A second request for a target that already has a live job joins that job;
// a different target, or the same one after the first finishes, gets its own.
func TestDedupJoinsTheLiveJobForTheSameTarget(t *testing.T) {
	ms := newMemStore()
	e := New(ms, quietLogger(), 1)
	e.Configure("backup.app", KindOptions{DedupKey: TargetKey("targetId")})
	ctx := context.Background()

	a, _ := e.Enqueue(ctx, "backup.app", map[string]any{"targetId": "t1", "userId": "alice"})
	b, _ := e.Enqueue(ctx, "backup.app", map[string]any{"targetId": "t1", "userId": "bob"})
	if a.ID != b.ID {
		t.Fatalf("same target queued twice: %d and %d", a.ID, b.ID)
	}
	c, _ := e.Enqueue(ctx, "backup.app", map[string]any{"targetId": "t2"})
	if c.ID == a.ID {
		t.Fatal("a different target joined the first target's job")
	}
	_ = ms.CompleteJob(ctx, a.ID)
	d, _ := e.Enqueue(ctx, "backup.app", map[string]any{"targetId": "t1"})
	if d.ID == a.ID {
		t.Fatal("a finished job still absorbed new requests")
	}
	// Kinds without a DedupKey are never merged.
	x, _ := e.Enqueue(ctx, "notify.deliver", map[string]any{"n": 1})
	y, _ := e.Enqueue(ctx, "notify.deliver", map[string]any{"n": 1})
	if x.ID == y.ID {
		t.Fatal("a kind without dedup was deduplicated")
	}
}

// A handler that overruns its kind's limit sees its context expire and the
// job is failed for good, not handed back for another identical attempt.
func TestTimeoutCancelsAndFailsTheJobWithoutRetry(t *testing.T) {
	ms := newMemStore()
	e := New(ms, quietLogger(), 1)
	e.Configure("slow", KindOptions{Timeout: 50 * time.Millisecond})
	sawDeadline := make(chan bool, 1)
	e.Register("slow", func(ctx context.Context, _ json.RawMessage) error {
		_, has := ctx.Deadline()
		sawDeadline <- has
		<-ctx.Done()
		return ctx.Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer func() { cancel(); e.Wait() }()
	job, _ := e.Enqueue(ctx, "slow", map[string]any{})
	e.Start(ctx)

	if !<-sawDeadline {
		t.Fatal("handler ran without a deadline")
	}
	waitFor(t, "timeout to be recorded", func() bool { st, _ := ms.status(job.ID); return st == "failed" })
	if _, msg := ms.status(job.ID); msg == "" {
		t.Fatal("timeout left no error message")
	}
}

// Every job gets a deadline even when its kind configures none.
func TestDefaultTimeoutApplies(t *testing.T) {
	ms := newMemStore()
	e := New(ms, quietLogger(), 1)
	got := make(chan time.Duration, 1)
	e.Register("plain", func(ctx context.Context, _ json.RawMessage) error {
		dl, ok := ctx.Deadline()
		if !ok {
			got <- 0
			return errors.New("no deadline")
		}
		got <- time.Until(dl)
		return nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer func() { cancel(); e.Wait() }()
	_, _ = e.Enqueue(ctx, "plain", nil)
	e.Start(ctx)
	if left := <-got; left <= DefaultTimeout-time.Minute || left > DefaultTimeout {
		t.Fatalf("deadline %s away, want about %s", left, DefaultTimeout)
	}
}

// The result of a job that finishes during shutdown is still written.
func TestOutcomeIsRecordedAfterShutdownCancels(t *testing.T) {
	ms := newMemStore()
	e := New(ms, quietLogger(), 1)
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	e.Register("finish", func(jctx context.Context, _ json.RawMessage) error {
		close(started)
		<-jctx.Done() // shutdown arrives mid-job
		return nil    // …and the handler still completes its work
	})
	job, _ := e.Enqueue(ctx, "finish", nil)
	e.Start(ctx)
	<-started
	cancel()
	e.Wait()
	if st, _ := ms.status(job.ID); st != "succeeded" {
		t.Fatalf("job is %s after shutdown, want succeeded", st)
	}
}
