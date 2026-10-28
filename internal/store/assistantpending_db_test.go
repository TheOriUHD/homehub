package store_test

import (
	"context"
	"encoding/json"
	"errors"
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

func put(t *testing.T, st *store.Store, userID uuid.UUID, msgID int64, summary string, expires time.Time) *store.PendingActions {
	t.Helper()
	row, err := st.PutPendingActions(context.Background(), userID, store.AssistantChannelTelegram, msgID, actions(summary), expires)
	if err != nil || row == nil {
		t.Fatalf("put %q: %v", summary, err)
	}
	return row
}

// A newer proposal replaces the older one. Two live proposals would mean a
// "yes" could run something the person had already moved on from.
func TestPendingActionsKeepOnlyTheNewest(t *testing.T) {
	st := dbStore(t)
	ctx := context.Background()
	userID, msgID := scratchThread(t, st)
	soon := time.Now().Add(15 * time.Minute)

	put(t, st, userID, msgID, "restart nginx", soon)
	newer := put(t, st, userID, msgID, "restart postgres", soon)

	held, err := st.TakePendingActions(ctx, userID, store.AssistantChannelTelegram, newer.Code())
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

// An approval names the proposal it answers. A yes meant for the older one
// must neither run the newer one nor remove it.
func TestApprovalForAReplacedProposalIsRefused(t *testing.T) {
	st := dbStore(t)
	ctx := context.Background()
	userID, msgID := scratchThread(t, st)
	soon := time.Now().Add(15 * time.Minute)

	older := put(t, st, userID, msgID, "restart nginx", soon)
	newer := put(t, st, userID, msgID, "reboot the host", soon)
	if older.Code() == newer.Code() {
		t.Fatalf("a replacement kept the old code %s", older.Code())
	}

	if held, err := st.TakePendingActions(ctx, userID, store.AssistantChannelTelegram, older.Code()); !errors.Is(err, store.ErrPendingSuperseded) || held != nil {
		t.Fatalf("stale code: held=%v err=%v, want ErrPendingSuperseded", held, err)
	}
	held, err := st.TakePendingActions(ctx, userID, store.AssistantChannelTelegram, newer.Code())
	if err != nil || held == nil || summaryOf(t, held.Actions) != "reboot the host" {
		t.Fatalf("the newer proposal did not survive a stale approval: %v %v", held, err)
	}
	if held.Code() != newer.Code() {
		t.Errorf("code changed across the round trip: %s vs %s", held.Code(), newer.Code())
	}
}

// Claiming consumes the proposal, so two approvals arriving together cannot
// both run it, and a second /yes finds nothing rather than running it again.
func TestPendingActionsAreClaimedOnce(t *testing.T) {
	st := dbStore(t)
	ctx := context.Background()
	userID, msgID := scratchThread(t, st)

	row := put(t, st, userID, msgID, "restart nginx", time.Now().Add(15*time.Minute))
	if held, err := st.TakePendingActions(ctx, userID, store.AssistantChannelTelegram, row.Code()); err != nil || held == nil {
		t.Fatalf("first take: %v %v", err, held)
	}
	held, err := st.TakePendingActions(ctx, userID, store.AssistantChannelTelegram, row.Code())
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

	row := put(t, st, userID, msgID, "restart nginx", time.Now().Add(-time.Minute))
	held, err := st.TakePendingActions(ctx, userID, store.AssistantChannelTelegram, row.Code())
	if err != nil || held == nil {
		t.Fatalf("take: %v %v", err, held)
	}
	if !time.Now().After(held.ExpiresAt) {
		t.Error("an old proposal did not read as expired")
	}
	if again, _ := st.TakePendingActions(ctx, userID, store.AssistantChannelTelegram, row.Code()); again != nil {
		t.Error("an expired proposal survived being claimed")
	}
}

// A new instruction supersedes the old proposal: clearing must leave nothing
// approvable behind.
func TestClearPendingActionsLeavesNothing(t *testing.T) {
	st := dbStore(t)
	ctx := context.Background()
	userID, msgID := scratchThread(t, st)

	row := put(t, st, userID, msgID, "restart nginx", time.Now().Add(15*time.Minute))
	if err := st.ClearPendingActions(ctx, userID, store.AssistantChannelTelegram); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if held, _ := st.TakePendingActions(ctx, userID, store.AssistantChannelTelegram, row.Code()); held != nil {
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

	row := put(t, st, userID, msgID, "restart nginx", time.Now().Add(15*time.Minute))
	if _, err := st.Pool().Exec(ctx, `delete from assistant_messages where id = $1`, msgID); err != nil {
		t.Fatalf("prune: %v", err)
	}
	if held, _ := st.TakePendingActions(ctx, userID, store.AssistantChannelTelegram, row.Code()); held != nil {
		t.Fatal("the proposal outlived the message it was going to answer")
	}
}
