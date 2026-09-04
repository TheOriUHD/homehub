package signals

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TheOriUHD/homehub/internal/store"
)

// These run against a real database (HOMEHUB_DATABASE_URL) because the
// reconciler's acknowledgement rules live in SQL. They use a collector name
// no live sweep registers, so the running app never touches the rows, and
// they delete what they wrote.
func dbStore(t *testing.T) *store.Store {
	t.Helper()
	dsn := os.Getenv("HOMEHUB_DATABASE_URL")
	if dsn == "" {
		t.Skip("HOMEHUB_DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return store.New(pool)
}

// dismiss acknowledges a row the way an operator would, minus the actor:
// dismissed_by references users, and the tests must not invent one.
func dismiss(t *testing.T, st *store.Store, id int64) {
	t.Helper()
	if _, err := st.Pool().Exec(context.Background(), `update signals set dismissed_at = now() where id = $1`, id); err != nil {
		t.Fatalf("dismiss: %v", err)
	}
}

func cleanupKey(t *testing.T, st *store.Store, key string) {
	t.Helper()
	t.Cleanup(func() {
		_, _ = st.Pool().Exec(context.Background(), `delete from signals where key = $1`, key)
	})
}

// A dismissed condition that is re-observed unchanged stays quiet; the same
// key observed as a recurrence surfaces as a fresh row.
func TestRecurAfterDismissalSurfacesFreshRow(t *testing.T) {
	st := dbStore(t)
	ctx := context.Background()
	key := "test-probe:" + uuid.NewString()
	cleanupKey(t, st, key)
	svc := NewService(st, func(string, any) {}, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	obs := Observation{Key: key, Severity: SevWarning, Title: "Probe failed", Body: "first",
		SourceType: "system", SourceName: "probe", AutoResolve: true,
		Detail: map[string]any{"runId": "a"}}
	svc.reconcileObservation(ctx, "test-probe", obs)
	first, err := st.GetLiveSignal(ctx, key)
	if err != nil || first == nil {
		t.Fatalf("expected a live row after the first observation: %v", err)
	}
	dismiss(t, st, first.ID)

	// Same run, re-observed: acknowledged, so nothing comes back.
	svc.reconcileObservation(ctx, "test-probe", obs)
	if live, _ := st.GetLiveSignal(ctx, key); live != nil {
		t.Fatalf("re-observing the acknowledged condition must stay quiet, got live row %d", live.ID)
	}

	// A newer run: the collector marks it as a recurrence.
	obs.Recur = true
	obs.Detail = map[string]any{"runId": "b"}
	svc.reconcileObservation(ctx, "test-probe", obs)
	second, _ := st.GetLiveSignal(ctx, key)
	if second == nil || second.ID == first.ID {
		t.Fatalf("a recurrence after dismissal must surface as a fresh live row, got %+v", second)
	}
}

// lastRunChanged compares the observed run against the row on record —
// live or acknowledged — and only a different run counts as new.
func TestLastRunChangedReadsLiveAndAcknowledgedRows(t *testing.T) {
	st := dbStore(t)
	ctx := context.Background()
	key := "test-probe:" + uuid.NewString()
	cleanupKey(t, st, key)
	c := &patchpilotCollector{Deps{Store: st}}
	runA, runB := uuid.New(), uuid.New()

	if c.lastRunChanged(ctx, key, runA) {
		t.Fatal("no row on record must not count as a change")
	}
	detail, _ := json.Marshal(map[string]any{"runId": runA})
	row, err := st.UpsertSignal(ctx, store.Signal{Key: key, Collector: "test-probe", SourceType: "system",
		SourceName: "probe", Severity: SevWarning, AutoResolve: true, Title: "t", Body: "b", Detail: detail}, false)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if c.lastRunChanged(ctx, key, runA) {
		t.Fatal("the same run as the live row is not a change")
	}
	if !c.lastRunChanged(ctx, key, runB) {
		t.Fatal("a different run than the live row is a change")
	}
	dismiss(t, st, row.ID)
	if c.lastRunChanged(ctx, key, runA) {
		t.Fatal("the same run as the acknowledged row is not a change")
	}
	if !c.lastRunChanged(ctx, key, runB) {
		t.Fatal("a different run than the acknowledged row is a change")
	}
}
