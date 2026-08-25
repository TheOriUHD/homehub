package calendar

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/TheOriUHD/homehub/internal/store"
)

func TestExpandWeeklyWithExdateAndOverride(t *testing.T) {
	calID := uuid.New()
	masterID := uuid.New()
	// Weekly Mondays 09:00 UTC starting 2026-08-03.
	master := store.CalendarEvent{
		ID: masterID, CalendarID: calID, UID: "u1",
		Title:    "Standup",
		StartsAt: time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC),
		EndsAt:   time.Date(2026, 8, 3, 9, 30, 0, 0, time.UTC),
		RRule:    "FREQ=WEEKLY;BYDAY=MO",
		Exdates:  []time.Time{time.Date(2026, 8, 17, 9, 0, 0, 0, time.UTC)},
	}
	movedSlot := time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)
	override := store.CalendarEvent{
		ID: uuid.New(), CalendarID: calID, UID: "u1", RecurrenceID: &movedSlot,
		Title:    "Standup (moved)",
		StartsAt: time.Date(2026, 8, 24, 14, 0, 0, 0, time.UTC),
		EndsAt:   time.Date(2026, 8, 24, 14, 30, 0, 0, time.UTC),
	}
	from := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	got := Expand([]store.CalendarEvent{master, override}, from, to)

	// Mondays in Aug 2026: 3, 10, 17(excluded), 24(moved), 31 → 4 instances.
	if len(got) != 4 {
		for _, g := range got {
			t.Logf("instance %s %s", g.Title, g.Start)
		}
		t.Fatalf("want 4 instances, got %d", len(got))
	}
	var moved *Instance
	for i := range got {
		if got[i].Title == "Standup (moved)" {
			moved = &got[i]
		}
		if got[i].Start.Equal(time.Date(2026, 8, 17, 9, 0, 0, 0, time.UTC)) {
			t.Fatal("EXDATE occurrence leaked into expansion")
		}
	}
	if moved == nil || !moved.Start.Equal(time.Date(2026, 8, 24, 14, 0, 0, 0, time.UTC)) {
		t.Fatal("override did not replace its slot")
	}
	if !moved.OccurrenceStart.Equal(movedSlot) {
		t.Fatalf("override occurrence slot wrong: %s", moved.OccurrenceStart)
	}
}

func TestICSRoundTrip(t *testing.T) {
	calID := uuid.New()
	rid := time.Date(2026, 9, 7, 9, 0, 0, 0, time.UTC)
	master := store.CalendarEvent{
		CalendarID: calID, UID: "abc@homehub",
		Title: "Weekly", Location: "Home", Description: "Notes here", URL: "https://example.com",
		StartsAt: time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC),
		EndsAt:   time.Date(2026, 8, 31, 10, 0, 0, 0, time.UTC),
		RRule:    "FREQ=WEEKLY;BYDAY=MO",
		Exdates:  []time.Time{time.Date(2026, 9, 14, 9, 0, 0, 0, time.UTC)},
	}
	override := store.CalendarEvent{
		CalendarID: calID, UID: "abc@homehub", RecurrenceID: &rid,
		Title:    "Weekly (special)",
		StartsAt: time.Date(2026, 9, 7, 11, 0, 0, 0, time.UTC),
		EndsAt:   time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC),
	}
	data, err := BuildICS(master, []store.CalendarEvent{override})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := ParseICS(calID, data, "/cal/abc.ics", "etag1")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("want 2 rows, got %d", len(rows))
	}
	var m, o *store.CalendarEvent
	for i := range rows {
		if rows[i].RecurrenceID == nil {
			m = &rows[i]
		} else {
			o = &rows[i]
		}
	}
	if m == nil || o == nil {
		t.Fatal("master/override split failed")
	}
	if m.Title != "Weekly" || m.RRule != "FREQ=WEEKLY;BYDAY=MO" || m.Location != "Home" || m.URL != "https://example.com" {
		t.Fatalf("master fields lost: %+v", m)
	}
	if len(m.Exdates) != 1 || !m.Exdates[0].Equal(master.Exdates[0]) {
		t.Fatalf("exdate lost: %v", m.Exdates)
	}
	if !o.RecurrenceID.Equal(rid) || !o.StartsAt.Equal(override.StartsAt) {
		t.Fatalf("override fields lost: %+v", o)
	}
}

func TestSetRRuleUntil(t *testing.T) {
	got := setRRuleUntil("FREQ=WEEKLY;COUNT=10;BYDAY=MO", time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	want := "FREQ=WEEKLY;BYDAY=MO;UNTIL=20260901T000000Z"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

// A feed is what a subscriber's client parses: it must round-trip through
// the same decoder, carry the calendar name, and keep rules and overrides
// as rules and overrides rather than pre-expanded copies.
func TestBuildFeedRoundTrips(t *testing.T) {
	calID := uuid.New()
	start := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	rows := []store.CalendarEvent{
		{CalendarID: calID, UID: "weekly@homehub", Title: "Bins out", StartsAt: start, EndsAt: start.Add(30 * time.Minute), RRule: "FREQ=WEEKLY;BYDAY=TU"},
		{CalendarID: calID, UID: "weekly@homehub", Title: "Bins out (moved)", StartsAt: start.AddDate(0, 0, 8), EndsAt: start.AddDate(0, 0, 8).Add(30 * time.Minute),
			RecurrenceID: ptrTime(start.AddDate(0, 0, 7))},
		{CalendarID: calID, UID: "holiday@homehub", Title: "Holiday", StartsAt: time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC), EndsAt: time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC), AllDay: true},
	}
	body, err := BuildFeed("Haus", rows, 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{"X-WR-CALNAME:Haus", "REFRESH-INTERVAL;VALUE=DURATION:PT15M", "RRULE:FREQ=WEEKLY;BYDAY=TU", "RECURRENCE-ID:20260908T080000Z", "DTSTART;VALUE=DATE:20261003"} {
		if !strings.Contains(text, want) {
			t.Fatalf("feed lacks %q:\n%s", want, text)
		}
	}
	back, err := ParseICS(calID, body, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 3 {
		t.Fatalf("expected the 3 rows back, got %d", len(back))
	}
}

func ptrTime(t time.Time) *time.Time { return &t }
