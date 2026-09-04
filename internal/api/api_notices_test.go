package api

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/TheOriUHD/homehub/internal/store"
)

// fakeNoticeStore is the gate's view of the store, in memory.
type fakeNoticeStore struct {
	notices   map[string]*store.SignalNotice
	scheduled map[string]time.Time
}

func newFakeNoticeStore() *fakeNoticeStore {
	return &fakeNoticeStore{notices: map[string]*store.SignalNotice{}, scheduled: map[string]time.Time{}}
}

func (f *fakeNoticeStore) GetSignalNotice(_ context.Context, key string) (*store.SignalNotice, error) {
	return f.notices[key], nil
}

func (f *fakeNoticeStore) ScheduleRecovery(_ context.Context, key string, _ int64, due time.Time) (bool, error) {
	n, ok := f.notices[key]
	if !ok || n.LastSeverity == "recovered" {
		return false, nil
	}
	f.scheduled[key] = due
	return true, nil
}

func (f *fakeNoticeStore) CancelRecovery(_ context.Context, key string) (bool, error) {
	_, ok := f.scheduled[key]
	delete(f.scheduled, key)
	return ok, nil
}

func TestGateParksRecoveryInsteadOfAnnouncingIt(t *testing.T) {
	st := newFakeNoticeStore()
	now := time.Now()
	st.notices["printer"] = &store.SignalNotice{Key: "printer", LastSentAt: now.Add(-2 * time.Hour), LastSeverity: "warning", SentCount: 1}

	proceed, why := gateProceeds(context.Background(), st, store.Signal{ID: 7, Key: "printer", Severity: "warning"}, "recovered", now)
	if proceed {
		t.Fatal("a recovery must never be announced straight away")
	}
	due, ok := st.scheduled["printer"]
	if !ok || due.Sub(now) != recoveryHold {
		t.Fatalf("recovery should be parked for %s, got %v (%s)", recoveryHold, due, why)
	}
}

func TestGateDropsRecoveryNobodyWasToldAbout(t *testing.T) {
	st := newFakeNoticeStore()
	proceed, why := gateProceeds(context.Background(), st, store.Signal{Key: "printer", Severity: "warning"}, "recovered", time.Now())
	if proceed || len(st.scheduled) != 0 {
		t.Fatalf("an all-clear for an unreported fault is not news: proceed=%v scheduled=%v why=%q", proceed, st.scheduled, why)
	}
}

// The flap the owner actually saw: told "down", signal clears, fault returns
// three minutes later. Before, "it's back" went out at once and the return
// counted as worse than "recovered" and went out too. Now the return
// withdraws the parked all-clear and is a rate-limited repeat.
func TestGateWithdrawsRecoveryWhenTheFaultReturns(t *testing.T) {
	st := newFakeNoticeStore()
	now := time.Now()
	st.notices["mc"] = &store.SignalNotice{Key: "mc", LastSentAt: now.Add(-20 * time.Minute), LastSeverity: "warning", SentCount: 1}
	sig := store.Signal{ID: 1, Key: "mc", Severity: "warning"}

	gateNotice(context.Background(), st, sig, "recovered", now)
	proceed, why := gateProceeds(context.Background(), st, sig, "new", now.Add(3*time.Minute))
	if proceed {
		t.Fatal("the fault returning inside the hold is the same outage continuing, not news")
	}
	if _, still := st.scheduled["mc"]; still {
		t.Fatal("the parked recovery must be withdrawn — it would contradict the next message")
	}
	if why == "" {
		t.Fatal("the log should say why")
	}
}

func TestGateLetsAnEscalationThroughTheFloor(t *testing.T) {
	st := newFakeNoticeStore()
	now := time.Now()
	st.notices["disk"] = &store.SignalNotice{Key: "disk", LastSentAt: now.Add(-5 * time.Minute), LastSeverity: "warning", SentCount: 1}
	proceed, _ := gateProceeds(context.Background(), st, store.Signal{Key: "disk", Severity: "critical"}, "escalated", now)
	if !proceed {
		t.Fatal("getting worse is exactly the repeat worth an interruption")
	}
	proceed, _ = gateProceeds(context.Background(), st, store.Signal{Key: "disk", Severity: "warning"}, "new", now)
	if proceed {
		t.Fatal("an unchanged repeat five minutes later must be dropped")
	}
}

func TestGateFirstReportPasses(t *testing.T) {
	st := newFakeNoticeStore()
	proceed, _ := gateProceeds(context.Background(), st, store.Signal{Key: "nas", Severity: "critical"}, "new", time.Now())
	if !proceed {
		t.Fatal("the first report of a condition must go out")
	}
}

func ptrTime(t time.Time) *time.Time { return &t }

// gateProceeds is the older tests' view: did the transition get through to
// the policy?
func gateProceeds(ctx context.Context, st noticeStore, sig store.Signal, event string, now time.Time) (bool, string) {
	d, why := gateNotice(ctx, st, sig, event, now)
	return d == gateSend, why
}

// A fault that comes back inside the flap window after its all-clear went
// out is the same trouble: no worse than what was reported, it is left for
// the digest instead of interrupting again.
func TestGateLeavesAReturnAfterAllClearForTheDigest(t *testing.T) {
	st := newFakeNoticeStore()
	now := time.Now()
	st.notices["k"] = &store.SignalNotice{Key: "k", LastSentAt: now.Add(-40 * time.Minute), LastSeverity: "recovered",
		LastFaultAt: ptrTime(now.Add(-2 * time.Hour)), LastFaultSeverity: "warning"}
	d, why := gateNotice(context.Background(), st, store.Signal{Key: "k", Severity: "warning"}, "new", now)
	if d != gateQueue || !strings.Contains(why, "left for the digest") {
		t.Fatalf("expected the return to be left for the digest, got d=%v why=%q", d, why)
	}
}

// Worse than the fault that was reported still gets through, even right
// after an all-clear.
func TestGateReturnWorseThanReportedPasses(t *testing.T) {
	st := newFakeNoticeStore()
	now := time.Now()
	st.notices["k"] = &store.SignalNotice{Key: "k", LastSentAt: now.Add(-5 * time.Minute), LastSeverity: "recovered",
		LastFaultAt: ptrTime(now.Add(-time.Hour)), LastFaultSeverity: "warning"}
	d, _ := gateNotice(context.Background(), st, store.Signal{Key: "k", Severity: "critical"}, "new", now)
	if d != gateSend {
		t.Fatal("a return worse than the reported fault must pass")
	}
}

// After a quiet stretch longer than the window, a return is news again.
func TestGateReturnAfterQuietStretchIsNews(t *testing.T) {
	st := newFakeNoticeStore()
	now := time.Now()
	st.notices["k"] = &store.SignalNotice{Key: "k", LastSentAt: now.Add(-7 * time.Hour), LastSeverity: "recovered",
		LastFaultAt: ptrTime(now.Add(-9 * time.Hour)), LastFaultSeverity: "warning"}
	d, why := gateNotice(context.Background(), st, store.Signal{Key: "k", Severity: "warning"}, "new", now)
	if d != gateSend {
		t.Fatalf("a return after a quiet stretch must pass, got %q", why)
	}
}

// A flapping key neither announces its all-clear nor its next return.
func TestGateSilencesAFlappingKey(t *testing.T) {
	st := newFakeNoticeStore()
	now := time.Now()
	st.notices["k"] = &store.SignalNotice{Key: "k", LastSentAt: now, LastSeverity: "warning",
		LastFaultAt: ptrTime(now), LastFaultSeverity: "warning", Flaps: 2, FlapWindowStart: ptrTime(now.Add(-3 * time.Hour))}
	if d, why := gateNotice(context.Background(), st, store.Signal{Key: "k", Severity: "warning"}, "recovered", now.Add(time.Minute)); d != gateQueue || !strings.Contains(why, "flapping") {
		t.Fatalf("flapping all-clear should be left for the digest, got d=%v why=%q", d, why)
	}
	if _, parked := st.scheduled["k"]; parked {
		t.Fatal("a flapping key's all-clear must not be parked either")
	}
	st.notices["k"].LastSeverity = "recovered"
	if d, why := gateNotice(context.Background(), st, store.Signal{Key: "k", Severity: "warning"}, "new", now.Add(40*time.Minute)); d != gateQueue || !strings.Contains(why, "flapping") {
		t.Fatalf("flapping return should be left for the digest, got d=%v why=%q", d, why)
	}
}
