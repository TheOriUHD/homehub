package assistant

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"homehub/internal/store"
)

// Editing an event has to MERGE. The endpoint behind it replaces the whole
// record, so a tool that sent only the changed fields would quietly blank the
// location, the notes and the link of every event it touched. Database-backed:
// run with HOMEHUB_DATABASE_URL set. The scratch calendar is removed after.

func editStore(t *testing.T) *store.Store {
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

// scratchEvent makes a throwaway calendar holding one full event.
func scratchEvent(t *testing.T, st *store.Store, rrule string) *store.CalendarEvent {
	t.Helper()
	ctx := context.Background()
	cal, err := st.CreateCalendar(ctx, store.Calendar{
		Name: "test-probe-" + uuid.NewString()[:8], Kind: "local", Color: "#888888",
		Settings: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("seed calendar: %v", err)
	}
	t.Cleanup(func() { _, _ = st.DeleteCalendar(context.Background(), cal.ID) })

	start := time.Date(2026, 10, 1, 14, 0, 0, 0, time.Local)
	ev, err := st.CreateCalendarEvent(ctx, store.CalendarEvent{
		CalendarID: cal.ID, UID: uuid.NewString(),
		Title: "Dentist", Location: "Hauptstrasse 3", Description: "bring the card",
		URL: "https://example.invalid/appt", RRule: rrule,
		StartsAt: start, EndsAt: start.Add(time.Hour), Exdates: []time.Time{},
	})
	if err != nil {
		t.Fatalf("seed event: %v", err)
	}
	return ev
}

// propose runs the tool and returns the merged event body it queued.
func propose(t *testing.T, st *store.Store, args map[string]any) (map[string]any, string) {
	t.Helper()
	tb := &Toolbox{Store: st}
	raw, _ := json.Marshal(args)
	out, err := tb.proposeUpdateEvent(context.Background(), Access{}, raw)
	if err != nil {
		t.Fatalf("propose: %v", err)
	}
	action, ok := out.(*PendingAction)
	if !ok {
		t.Fatalf("an edit must be queued for approval, got %T", out)
	}
	if action.Kind != "calendar.update_event" {
		t.Fatalf("kind = %q", action.Kind)
	}
	var params struct {
		Event map[string]any `json:"event"`
	}
	if err := json.Unmarshal(action.Params, &params); err != nil {
		t.Fatal(err)
	}
	return params.Event, action.Summary
}

// The whole point: everything not mentioned survives.
func TestEditKeepsTheFieldsItWasNotAskedAbout(t *testing.T) {
	st := editStore(t)
	ev := scratchEvent(t, st, "")

	event, summary := propose(t, st, map[string]any{"eventId": ev.ID.String(), "title": "Dentist check-up"})

	if event["title"] != "Dentist check-up" {
		t.Errorf("title = %v", event["title"])
	}
	for field, want := range map[string]any{
		"location":    "Hauptstrasse 3",
		"description": "bring the card",
		"url":         "https://example.invalid/appt",
	} {
		if event[field] != want {
			t.Errorf("%s = %v, want %v — an unmentioned field was wiped", field, event[field], want)
		}
	}
	if event["startsAt"] != ev.StartsAt.Format(time.RFC3339) {
		t.Errorf("startsAt moved on a rename: %v", event["startsAt"])
	}
	if summary == "" {
		t.Error("the confirmation needs to say what changes")
	}
}

// "Move it to 3pm" means the appointment keeps its length.
func TestMovingTheStartCarriesTheEnd(t *testing.T) {
	st := editStore(t)
	ev := scratchEvent(t, st, "")

	event, _ := propose(t, st, map[string]any{"eventId": ev.ID.String(), "start": "2026-10-01 15:00"})

	start, _ := time.Parse(time.RFC3339, event["startsAt"].(string))
	end, _ := time.Parse(time.RFC3339, event["endsAt"].(string))
	if got := end.Sub(start); got != time.Hour {
		t.Fatalf("duration became %s; moving the start must not stretch or squash the event", got)
	}
	if start.Hour() != 15 {
		t.Fatalf("start = %s, want 15:00", start)
	}
}

// An explicit end is honoured rather than recomputed.
func TestAnExplicitEndWins(t *testing.T) {
	st := editStore(t)
	ev := scratchEvent(t, st, "")

	event, _ := propose(t, st, map[string]any{
		"eventId": ev.ID.String(), "start": "2026-10-01 15:00", "end": "2026-10-01 17:30",
	})
	end, _ := time.Parse(time.RFC3339, event["endsAt"].(string))
	if end.Hour() != 17 || end.Minute() != 30 {
		t.Fatalf("end = %s, want 17:30", end)
	}
}

// Clearing a field is a real instruction and must be distinguishable from
// leaving it alone — the difference between "" and absent.
func TestAFieldCanBeCleared(t *testing.T) {
	st := editStore(t)
	ev := scratchEvent(t, st, "")

	event, summary := propose(t, st, map[string]any{"eventId": ev.ID.String(), "location": ""})
	if event["location"] != "" {
		t.Fatalf("location = %v, want it cleared", event["location"])
	}
	if event["description"] != "bring the card" {
		t.Fatalf("clearing one field disturbed another: %v", event["description"])
	}
	if !strings.Contains(summary, "clear the location") {
		t.Errorf("summary should say what it clears: %q", summary)
	}
}

// An edit that changes nothing is a mistake worth reporting, not a no-op
// queued for the user to approve.
func TestAnEditThatChangesNothingIsRefused(t *testing.T) {
	st := editStore(t)
	ev := scratchEvent(t, st, "")

	tb := &Toolbox{Store: st}
	raw, _ := json.Marshal(map[string]any{"eventId": ev.ID.String(), "title": "Dentist"})
	if _, err := tb.proposeUpdateEvent(context.Background(), Access{}, raw); err == nil {
		t.Fatal("re-sending the same title was queued as a change")
	}
}

// A backwards edit is caught before anyone is asked to approve it.
func TestAnEndBeforeTheStartIsRefused(t *testing.T) {
	st := editStore(t)
	ev := scratchEvent(t, st, "")

	tb := &Toolbox{Store: st}
	raw, _ := json.Marshal(map[string]any{"eventId": ev.ID.String(), "end": "2026-10-01 09:00"})
	if _, err := tb.proposeUpdateEvent(context.Background(), Access{}, raw); err == nil {
		t.Fatal("an end before the start was accepted")
	}
}

// On a repeating event the scope decides how much moves, so the confirmation
// has to say which — "move it to 3pm" meaning every future Monday is not a
// surprise anyone should get after the fact.
func TestRepeatingEditsNameTheirScope(t *testing.T) {
	st := editStore(t)
	ev := scratchEvent(t, st, "FREQ=WEEKLY")

	for scope, want := range map[string]string{
		"this":      "this occurrence only",
		"following": "this and all following",
		"all":       "the whole repeating series",
	} {
		_, summary := propose(t, st, map[string]any{
			"eventId": ev.ID.String(), "start": "2026-10-01 15:00", "scope": scope,
		})
		if !strings.Contains(summary, want) {
			t.Errorf("scope %q summary = %q, want it to mention %q", scope, summary, want)
		}
	}
}
