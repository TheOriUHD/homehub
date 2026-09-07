package store_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"homehub/internal/store"
)

// Database-backed: run with HOMEHUB_DATABASE_URL set. The rules being checked
// are the safety of the /yes flow — the newest proposal is the only one that
// can be approved, approving it consumes it, and a stale one is recognisable
// as stale rather than run.

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

// scratchThread makes a user and one assistant message to hang proposals off,
// then removes them. It never touches a real account.
func scratchThread(t *testing.T, st *store.Store) (uuid.UUID, int64) {
	t.Helper()
	ctx := context.Background()
	username := "test-probe-" + uuid.NewString()[:8]
	var userID uuid.UUID
	err := st.Pool().QueryRow(ctx,
		`insert into users (username, display_name, password_hash, status)
		 values ($1, 'Pending probe', 'x', 'disabled') returning id`, username).Scan(&userID)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() { _, _ = st.Pool().Exec(ctx, `delete from users where id = $1`, userID) })

	msgID, err := st.AddAssistantMessage(ctx, userID, store.AssistantChannelTelegram, "",
		"assistant", "probe", nil, nil, nil)
	if err != nil {
		t.Fatalf("seed message: %v", err)
	}
	return userID, msgID
}

func actions(summary string) json.RawMessage {
	raw, _ := json.Marshal([]map[string]string{{"kind": "ssh.run", "summary": summary}})
	return raw
}

func summaryOf(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var got []map[string]string
	if err := json.Unmarshal(raw, &got); err != nil || len(got) != 1 {
		t.Fatalf("stored actions did not round-trip: %v %s", err, raw)
	}
	return got[0]["summary"]
}

// A newer proposal replaces the older one. Two live proposals would mean a
// "yes" could run something the person had already moved on from.
func TestPendingActionsKeepOnlyTheNewest(t *testing.T) {
	st := dbStore(t)
	ctx := context.Background()
	userID, msgID := scratchThread(t, st)
	soon := time.Now().Add(15 * time.Minute)

	if err := st.PutPendingActions(ctx, userID, store.AssistantChannelTelegram, msgID, actions("restart nginx"), soon); err != nil {
		t.Fatalf("first put: %v", err)
	}
	if err := st.PutPendingActions(ctx, userID, store.AssistantChannelTelegram, msgID, actions("restart postgres"), soon); err != nil {
		t.Fatalf("second put: %v", err)
	}

	held, err := st.TakePendingActions(ctx, userID, store.AssistantChannelTelegram)
	if err != nil || held == nil {
		t.Fatalf("take: %v %v", err, held)
	}
	if got := summaryOf(t, held.Actions); got != "restart postgres" {
		t.Fatalf("held %q; the newer proposal must be the one that survives", got)
	}
	if held.ParentID != msgID {
		t.Fatalf("parent = %d, want %d — the resumed answer would land off-thread", held.ParentID, msgID)
	}
}

// Claiming consumes the proposal, so two approvals arriving together cannot
// both run it, and a second /yes finds nothing rather than running it again.
func TestPendingActionsAreClaimedOnce(t *testing.T) {
	st := dbStore(t)
	ctx := context.Background()
	userID, msgID := scratchThread(t, st)

	if err := st.PutPendingActions(ctx, userID, store.AssistantChannelTelegram, msgID,
		actions("restart nginx"), time.Now().Add(15*time.Minute)); err != nil {
		t.Fatalf("put: %v", err)
	}
	if held, err := st.TakePendingActions(ctx, userID, store.AssistantChannelTelegram); err != nil || held == nil {
		t.Fatalf("first take: %v %v", err, held)
	}
	held, err := st.TakePendingActions(ctx, userID, store.AssistantChannelTelegram)
	if err != nil {
		t.Fatalf("second take: %v", err)
	}
	if held != nil {
		t.Fatal("the same proposal was claimed twice — one approval would run the command twice")
	}
}

// An expired proposal still comes back, so the bridge can say it expired. It
// must come back marked as expired, and it must be gone afterwards.
func TestExpiredPendingActionsAreReturnedAndRemoved(t *testing.T) {
	st := dbStore(t)
	ctx := context.Background()
	userID, msgID := scratchThread(t, st)

	if err := st.PutPendingActions(ctx, userID, store.AssistantChannelTelegram, msgID,
		actions("restart nginx"), time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("put: %v", err)
	}
	held, err := st.TakePendingActions(ctx, userID, store.AssistantChannelTelegram)
	if err != nil || held == nil {
		t.Fatalf("take: %v %v", err, held)
	}
	if !time.Now().After(held.ExpiresAt) {
		t.Error("an old proposal did not read as expired")
	}
	if again, _ := st.TakePendingActions(ctx, userID, store.AssistantChannelTelegram); again != nil {
		t.Error("an expired proposal survived being claimed")
	}
}

// A new instruction supersedes the old proposal: clearing must leave nothing
// approvable behind.
func TestClearPendingActionsLeavesNothing(t *testing.T) {
	st := dbStore(t)
	ctx := context.Background()
	userID, msgID := scratchThread(t, st)

	if err := st.PutPendingActions(ctx, userID, store.AssistantChannelTelegram, msgID,
		actions("restart nginx"), time.Now().Add(15*time.Minute)); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := st.ClearPendingActions(ctx, userID, store.AssistantChannelTelegram); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if held, _ := st.TakePendingActions(ctx, userID, store.AssistantChannelTelegram); held != nil {
		t.Fatal("a cleared proposal was still approvable")
	}
	// Clearing nothing is the common case (most turns propose nothing).
	if err := st.ClearPendingActions(ctx, userID, store.AssistantChannelTelegram); err != nil {
		t.Fatalf("clearing an empty slot must be harmless: %v", err)
	}
}

// The proposal hangs off the message that made it. If the thread is pruned,
// the proposal has nothing left to resume onto and must go with it.
func TestPendingActionsGoWithTheirThread(t *testing.T) {
	st := dbStore(t)
	ctx := context.Background()
	userID, msgID := scratchThread(t, st)

	if err := st.PutPendingActions(ctx, userID, store.AssistantChannelTelegram, msgID,
		actions("restart nginx"), time.Now().Add(15*time.Minute)); err != nil {
		t.Fatalf("put: %v", err)
	}
	if _, err := st.Pool().Exec(ctx, `delete from assistant_messages where id = $1`, msgID); err != nil {
		t.Fatalf("prune: %v", err)
	}
	if held, _ := st.TakePendingActions(ctx, userID, store.AssistantChannelTelegram); held != nil {
		t.Fatal("the proposal outlived the message it was going to answer")
	}
}
