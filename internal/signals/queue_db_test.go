package signals

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/TheOriUHD/homehub/internal/store"
)

// The digest queue keeps one live row per key: repeats refresh it, an
// all-clear marks it cleared, a return after that counts a bounce, and a
// flush stamps it out of the way. Database-backed, private key, cleaned up.
func TestNoticeQueueKeepsOneLiveRowPerKey(t *testing.T) {
	st := dbStore(t)
	ctx := context.Background()
	key := "test-probe:" + uuid.NewString()
	t.Cleanup(func() { _, _ = st.Pool().Exec(ctx, `delete from notice_queue where key = $1`, key) })

	first, err := st.EnqueueNotice(ctx, store.QueuedNotice{Key: key, Event: "new", Severity: "warning", Title: "Camera A broke", SourceName: "NVR"})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	second, err := st.EnqueueNotice(ctx, store.QueuedNotice{Key: key, Event: "escalated", Severity: "critical", Title: "Camera A dead", SourceName: "NVR"})
	if err != nil || second.ID != first.ID || second.Severity != "critical" || second.Title != "Camera A dead" {
		t.Fatalf("a repeat must refresh the same row: %v %+v", err, second)
	}
	if resolved, _ := st.ResolveQueuedNotice(ctx, key); !resolved {
		t.Fatal("an all-clear resolves the live row")
	}
	third, _ := st.EnqueueNotice(ctx, store.QueuedNotice{Key: key, Event: "new", Severity: "warning", Title: "Camera A broke", SourceName: "NVR"})
	if third.ID != first.ID || third.Bounces != 1 || third.ResolvedAt != nil {
		t.Fatalf("a return after clearing bounces the same row: %+v", third)
	}
	items, _ := st.ListQueuedNotices(ctx)
	found := false
	for _, q := range items {
		if q.Key == key {
			found = true
		}
	}
	if !found {
		t.Fatal("the row is listed while unflushed")
	}
	if err := st.FlushQueuedNotices(ctx, []int64{first.ID}, "digest"); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if resolved, _ := st.ResolveQueuedNotice(ctx, key); resolved {
		t.Fatal("a flushed row is out of the way")
	}
	fresh, _ := st.EnqueueNotice(ctx, store.QueuedNotice{Key: key, Event: "new", Severity: "warning", Title: "again"})
	if fresh.ID == first.ID || fresh.Bounces != 0 {
		t.Fatalf("after a flush the next fault starts a fresh row: %+v", fresh)
	}
	_, _ = st.Pool().Exec(ctx, `delete from notice_queue where key = $1`, key)
}
