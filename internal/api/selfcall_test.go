package api

import (
	"context"
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
