package signals

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// The notice record keeps the last fault apart from the last message and
// counts fault→clear→fault cycles inside the flap window. Database-backed
// (HOMEHUB_DATABASE_URL), private key, cleaned up.
func TestRecordSignalNoticeTracksFaultsAndFlaps(t *testing.T) {
	st := dbStore(t)
	ctx := context.Background()
	key := "test-probe:" + uuid.NewString()
	t.Cleanup(func() { _, _ = st.Pool().Exec(ctx, `delete from signal_notices where key = $1`, key) })
	window := 6 * time.Hour

	must := func(sev string) {
		if err := st.RecordSignalNotice(ctx, key, sev, window); err != nil {
			t.Fatalf("record %s: %v", sev, err)
		}
	}
	must("warning")
	n, _ := st.GetSignalNotice(ctx, key)
	if n == nil || n.LastFaultSeverity != "warning" || n.LastFaultAt == nil || n.Flaps != 0 {
		t.Fatalf("after the first fault: %+v", n)
	}
	must("recovered")
	n, _ = st.GetSignalNotice(ctx, key)
	if n.LastSeverity != "recovered" || n.LastFaultSeverity != "warning" || n.Flaps != 0 {
		t.Fatalf("an all-clear keeps the last fault on record and adds no flap: %+v", n)
	}
	must("warning")
	n, _ = st.GetSignalNotice(ctx, key)
	if n.Flaps != 1 || n.FlapWindowStart == nil {
		t.Fatalf("a fault right after its all-clear is the first flap: %+v", n)
	}
	must("recovered")
	must("critical")
	n, _ = st.GetSignalNotice(ctx, key)
	if n.Flaps != 2 || n.LastFaultSeverity != "critical" {
		t.Fatalf("second cycle counts, and the last fault follows the latest severity: %+v", n)
	}
	// A fault following a fault (no all-clear between) is not a flap.
	must("critical")
	n, _ = st.GetSignalNotice(ctx, key)
	if n.Flaps != 2 {
		t.Fatalf("a repeat without an all-clear between is not a flap: %+v", n)
	}
}
