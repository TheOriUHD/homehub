package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"homehub/internal/store"
)

// Database-backed: run with HOMEHUB_DATABASE_URL set. Any real database
// already has users, which is exactly the case pinned here — the test never
// creates an account, it proves none is created.

// Once any user exists, first-owner creation must refuse and insert nothing,
// however it is called. This is the guard that makes the setup wizard unable
// to mint a second Owner.
func TestCreateFirstOwnerRefusesOnceSetUp(t *testing.T) {
	st := dbStore(t)
	ctx := context.Background()
	n, err := st.CountUsers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Skip("empty database: this test only runs against an installed instance")
	}
	username := "test-probe-" + uuid.NewString()[:8]
	_, err = st.CreateFirstOwner(ctx, username, "probe", "x", "Owner")
	if !errors.Is(err, store.ErrAlreadySetUp) {
		t.Fatalf("got %v, want ErrAlreadySetUp", err)
	}
	if _, err := st.GetUserByUsername(ctx, username); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("a user was created anyway (lookup err=%v)", err)
	}
}
