package api

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"homehub/internal/httpctx"
	"homehub/internal/store"
)

// A zero Server proves the routing + strict-handler layers of SelfCall:
// reaching the real handler with nil deps panics, which the recovery maps
// to 500. A 400 before that means the transport layer itself is broken.
func TestSelfCallReachesHandlers(t *testing.T) {
	s := &Server{logger: slog.Default()}
	id := &httpctx.Identity{User: &store.User{ID: uuid.New(), DisplayName: "t"}}
	status, body := s.SelfCall(context.Background(), id, "GET",
		"/docker/"+uuid.NewString()+"/containers", nil)
	t.Logf("status=%d body=%s", status, body)
	if status == 400 {
		t.Fatalf("transport-layer 400 before handler: %s", body)
	}
}

// The agent's changes are announced so open pages catch up on their own. What
// it merely READ must not be: a turn runs several tool calls, and a refetch
// storm on every lookup is worse than the stale view it was meant to fix.
func TestOnlyRealChangesAreAnnounced(t *testing.T) {
	for _, tc := range []struct {
		method string
		code   int
		want   bool
	}{
		{http.MethodPost, 200, true},
		{http.MethodPut, 204, true},
		{http.MethodPatch, 200, true},
		{http.MethodDelete, 204, true},
		{http.MethodGet, 200, false},
		{http.MethodHead, 200, false},
		// A refusal moved nothing, whatever it was asking for.
		{http.MethodPost, 403, false},
		{http.MethodDelete, 404, false},
		{http.MethodPut, 500, false},
	} {
		if got := announcesChange(tc.method, tc.code); got != tc.want {
			t.Errorf("%s %d announced=%v, want %v", tc.method, tc.code, got, tc.want)
		}
	}
}

// The approved edit is replayed as a PUT built by hand, so its body has to
// decode into what the endpoint actually accepts. A silent mismatch here
// would fail on the owner's first edit, after they had already approved it.
func TestApprovedCalendarEditMatchesTheEndpointsBody(t *testing.T) {
	// Exactly the shape applyAssistantAction sends, with the merged event the
	// tool produced.
	sent := map[string]any{
		"scope":           "following",
		"occurrenceStart": "2026-10-01T14:00:00Z",
		"event": map[string]any{
			"title":       "Dentist check-up",
			"startsAt":    "2026-10-01T15:00:00Z",
			"endsAt":      "2026-10-01T16:00:00Z",
			"allDay":      false,
			"location":    "Hauptstrasse 3",
			"description": "bring the card",
			"url":         "https://example.invalid/appt",
			"rrule":       "FREQ=WEEKLY",
		},
	}
	raw, err := json.Marshal(sent)
	if err != nil {
		t.Fatal(err)
	}
	var body UpdateCalendarEventJSONRequestBody
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		t.Fatalf("the endpoint would reject the body the approval sends: %v", err)
	}
	if body.Event.Title != "Dentist check-up" {
		t.Errorf("title did not land: %q", body.Event.Title)
	}
	if body.Event.StartsAt.UTC().Hour() != 15 || body.Event.EndsAt.UTC().Hour() != 16 {
		t.Errorf("times did not land: %s → %s", body.Event.StartsAt, body.Event.EndsAt)
	}
	// The merged fields are the whole point: they must survive the round trip
	// rather than arriving nil and blanking the stored event.
	for name, got := range map[string]*string{
		"location":    body.Event.Location,
		"description": body.Event.Description,
		"url":         body.Event.Url,
		"rrule":       body.Event.Rrule,
	} {
		if got == nil || *got == "" {
			t.Errorf("%s arrived empty — an approved edit would wipe it", name)
		}
	}
	if body.Scope == nil || *body.Scope != "following" {
		t.Errorf("scope did not land: %v", body.Scope)
	}
	if body.OccurrenceStart == nil {
		t.Error("occurrenceStart did not land; 'this and following' would edit the whole series")
	}
}
