package patchpilot

import (
	"testing"
	"time"
)

func TestNextRunNthWeekday(t *testing.T) {
	// Reference: Wed 2026-08-19 03:10 local (UTC for the test).
	base := time.Date(2026, 8, 19, 3, 10, 0, 0, time.UTC)

	cases := []struct {
		name string
		expr string
		want time.Time
	}{
		// First Saturday of the month at 05:00. Aug 2026's first Saturday is
		// the 1st, already past on the 19th, so the next is Sep 5, 2026.
		{"first-saturday", "0 5 * * 6#1", time.Date(2026, 9, 5, 5, 0, 0, 0, time.UTC)},
		// Third Saturday: Aug 15 is past → Sep 19, 2026.
		{"third-saturday", "0 5 * * 6#3", time.Date(2026, 9, 19, 5, 0, 0, 0, time.UTC)},
		// Last Saturday of Aug 2026 is the 29th, still ahead of the 19th.
		{"last-saturday", "0 5 * * 6L", time.Date(2026, 8, 29, 5, 0, 0, 0, time.UTC)},
		// First Sunday: Aug 2 past → Sep 6, 2026.
		{"first-sunday", "30 4 * * 0#1", time.Date(2026, 9, 6, 4, 30, 0, 0, time.UTC)},
		// Fourth Wednesday of Aug 2026 is the 26th, still ahead.
		{"fourth-wednesday", "0 5 * * 3#4", time.Date(2026, 8, 26, 5, 0, 0, 0, time.UTC)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := nextRunAfter(c.expr, base)
			if err != nil {
				t.Fatalf("nextRunAfter(%q): %v", c.expr, err)
			}
			if !got.Equal(c.want) {
				t.Fatalf("nextRunAfter(%q) = %s, want %s", c.expr, got, c.want)
			}
		})
	}
}

func TestNextRunStandardStillWorks(t *testing.T) {
	base := time.Date(2026, 8, 19, 3, 10, 0, 0, time.UTC)
	// Weekly Sunday 04:00 → next Sunday is Aug 23, 2026.
	got, err := nextRunAfter("0 4 * * 0", base)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 8, 23, 4, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestNextRunRejectsBadExtension(t *testing.T) {
	// A day-of-month restriction combined with the nth-weekday form is ambiguous
	// and must be rejected rather than silently mis-scheduled.
	if _, err := nextRunAfter("0 5 15 * 6#1", time.Now()); err == nil {
		t.Fatal("expected error for dom + nth-weekday combination")
	}
	// Garbage stays an error.
	if _, err := nextRunAfter("not a cron", time.Now()); err == nil {
		t.Fatal("expected error for invalid cron")
	}
}
