package store_test

import (
	"strings"
	"testing"
	"time"

	"homehub/internal/store"
)

// The proposal code is what a /yes names. It has to be stable for one row
// (the bot shows it, the approval checks it later) and different for the row
// that replaces it, even when the replacement hangs off the same message.
func TestProposalCodeNamesOneRow(t *testing.T) {
	at := time.Date(2026, 10, 28, 9, 30, 0, 123456000, time.UTC)
	a := &store.PendingActions{ParentID: 42, CreatedAt: at}
	same := &store.PendingActions{ParentID: 42, CreatedAt: at.Local()}
	later := &store.PendingActions{ParentID: 42, CreatedAt: at.Add(time.Microsecond)}

	if a.Code() != same.Code() {
		t.Errorf("one row, two codes: %s vs %s", a.Code(), same.Code())
	}
	if a.Code() == later.Code() {
		t.Errorf("a replacement kept the code %s", a.Code())
	}
	code := a.Code()
	if len(code) != 4 {
		t.Fatalf("code %q is not four characters", code)
	}
	for _, r := range code {
		if !strings.ContainsRune("ABCDEFGHJKLMNPQRSTUVWXYZ23456789", r) {
			t.Errorf("code %q uses %q, which is easy to misread", code, r)
		}
	}
}
