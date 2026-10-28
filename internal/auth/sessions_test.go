package auth

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSessionExpiry_RollsButNeverPastTheCap(t *testing.T) {
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ttl := 30 * 24 * time.Hour

	if got := sessionExpiry(created, created, ttl); !got.Equal(created.Add(ttl)) {
		t.Fatalf("fresh session: %v", got)
	}
	// Day 80: rolling would give day 110, the cap says day 90.
	now := created.Add(80 * 24 * time.Hour)
	if got := sessionExpiry(created, now, ttl); !got.Equal(created.Add(sessionMaxLifetime)) {
		t.Fatalf("near the cap: got %v, want %v", got, created.Add(sessionMaxLifetime))
	}
	// A TTL configured longer than the cap is still capped.
	if got := sessionExpiry(created, created, 365*24*time.Hour); got.After(created.Add(sessionMaxLifetime)) {
		t.Fatalf("TTL beyond the cap was honoured: %v", got)
	}
}

func TestTrackedConnectionsCloseWithTheirSession(t *testing.T) {
	s := &Service{conns: &liveConns{bySession: map[uuid.UUID]map[uint64]trackedConn{}}}
	alice, bob := uuid.New(), uuid.New()
	sessA1, sessA2, sessB := uuid.New(), uuid.New(), uuid.New()

	var killedA1, killedA2, killedB atomic.Int32
	s.TrackConnection(sessA1, alice, func() { killedA1.Add(1) })
	s.TrackConnection(sessA1, alice, func() { killedA1.Add(1) }) // a second tab
	s.TrackConnection(sessA2, alice, func() { killedA2.Add(1) })
	untrackB := s.TrackConnection(sessB, bob, func() { killedB.Add(1) })

	// Logging out session A1 closes both of its sockets and nothing else.
	s.closeConns(func(sid uuid.UUID, _ trackedConn) bool { return sid == sessA1 })
	if killedA1.Load() != 2 || killedA2.Load() != 0 || killedB.Load() != 0 {
		t.Fatalf("after A1 logout: a1=%d a2=%d b=%d", killedA1.Load(), killedA2.Load(), killedB.Load())
	}
	// Kills are once-only even if the session is revoked again.
	s.closeConns(func(sid uuid.UUID, _ trackedConn) bool { return sid == sessA1 })
	if killedA1.Load() != 2 {
		t.Fatalf("a socket was killed twice: %d", killedA1.Load())
	}

	// "Sign out everywhere else" for alice from A1 reaches A2, not bob.
	except := sessA1
	s.closeConns(func(sid uuid.UUID, c trackedConn) bool {
		return c.userID == alice && sid != except
	})
	if killedA2.Load() != 1 || killedB.Load() != 0 {
		t.Fatalf("after revoke-others: a2=%d b=%d", killedA2.Load(), killedB.Load())
	}

	// An untracked (already closed) connection is not killed later.
	untrackB()
	s.closeConns(func(uuid.UUID, trackedConn) bool { return true })
	if killedB.Load() != 0 {
		t.Fatal("an untracked connection was killed")
	}
	if _, ok := s.conns.bySession[sessB]; ok {
		t.Fatal("untrack left an empty entry behind")
	}
}
