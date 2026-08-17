package api

import (
	"context"
	"log/slog"
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
