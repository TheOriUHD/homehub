package store_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"homehub/internal/store"
)

// Database-backed: run with HOMEHUB_DATABASE_URL set. Works on a scratch
// user and removes it (its invite goes with it); never touches a real
// account.

// Two people racing the same invite link: exactly one redemption may win,
// and the stored password must be the winner's.
func TestRedeemInviteIsSingleUse(t *testing.T) {
	st := dbStore(t)
	ctx := context.Background()

	username := "test-probe-" + uuid.NewString()[:8]
	var userID uuid.UUID
	if err := st.Pool().QueryRow(ctx,
		`insert into users (username, display_name, status) values ($1, 'Invite probe', 'active') returning id`,
		username).Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() { _ = st.DeleteUser(ctx, userID) })

	hash := []byte("invite-probe-" + uuid.NewString())
	if err := st.CreateInvite(ctx, userID, hash, userID, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("create invite: %v", err)
	}
	invite, _, err := st.InviteByTokenHash(ctx, hash)
	if err != nil {
		t.Fatalf("lookup invite: %v", err)
	}

	const racers = 8
	var wg sync.WaitGroup
	results := make([]error, racers)
	for i := range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = st.RedeemInvite(ctx, invite.ID, userID, "hash-"+string(rune('a'+i)))
		}()
	}
	wg.Wait()

	winner := -1
	for i, err := range results {
		switch {
		case err == nil:
			if winner >= 0 {
				t.Fatalf("racers %d and %d both redeemed the invite", winner, i)
			}
			winner = i
		case !errors.Is(err, store.ErrNotFound):
			t.Fatalf("racer %d: %v", i, err)
		}
	}
	if winner < 0 {
		t.Fatal("nobody redeemed the invite")
	}
	u, err := st.GetUser(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if u.PasswordHash == nil || *u.PasswordHash != "hash-"+string(rune('a'+winner)) {
		t.Fatalf("stored password is not the winner's: %v", u.PasswordHash)
	}
	if err := st.RedeemInvite(ctx, invite.ID, userID, "late"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("a used invite redeemed again: %v", err)
	}
}
