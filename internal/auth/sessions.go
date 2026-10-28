package auth

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"homehub/internal/store"
)

// sessionMaxLifetime is the absolute cap on a session, however active it
// stays. The rolling TTL alone let a session that is used at least once a
// month live forever — and with it a stolen cookie.
const sessionMaxLifetime = 90 * 24 * time.Hour

// sessionExpiry is the next expires_at for a session created at created:
// the rolling TTL from now, never past the absolute cap.
func sessionExpiry(created, now time.Time, ttl time.Duration) time.Time {
	exp := now.Add(ttl)
	if limit := created.Add(sessionMaxLifetime); exp.After(limit) {
		return limit
	}
	return exp
}

// liveConns tracks long-lived connections (event socket, terminals,
// consoles) by the session that opened them, so revoking a session also
// closes them. A WebSocket authenticates once at upgrade; without this an
// open terminal outlived the logout that should have ended it.
type liveConns struct {
	mu        sync.Mutex
	next      uint64
	bySession map[uuid.UUID]map[uint64]trackedConn
}

type trackedConn struct {
	userID uuid.UUID
	kill   func()
}

// TrackConnection registers a connection opened under sessionID; kill is
// called (once, from any goroutine) when that session is revoked. The
// returned func unregisters it and must be called when the connection ends.
func (s *Service) TrackConnection(sessionID, userID uuid.UUID, kill func()) (untrack func()) {
	c := s.conns
	c.mu.Lock()
	defer c.mu.Unlock()
	c.next++
	key := c.next
	if c.bySession[sessionID] == nil {
		c.bySession[sessionID] = map[uint64]trackedConn{}
	}
	c.bySession[sessionID][key] = trackedConn{userID: userID, kill: sync.OnceFunc(kill)}
	return func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		delete(c.bySession[sessionID], key)
		if len(c.bySession[sessionID]) == 0 {
			delete(c.bySession, sessionID)
		}
	}
}

// closeConns kills every tracked connection matching the filter. Kills run
// outside the lock: they may block on a network close.
func (s *Service) closeConns(match func(sessionID uuid.UUID, c trackedConn) bool) {
	s.conns.mu.Lock()
	var kills []func()
	for sid, conns := range s.conns.bySession {
		for _, c := range conns {
			if match(sid, c) {
				kills = append(kills, c.kill)
			}
		}
	}
	s.conns.mu.Unlock()
	for _, k := range kills {
		k()
	}
}

// RevokeSession revokes one session and closes its open connections.
func (s *Service) RevokeSession(ctx context.Context, sessionID uuid.UUID) error {
	err := s.store.RevokeSession(ctx, sessionID)
	// Close even when the row was already revoked: the sockets are what's
	// left of it.
	s.closeConns(func(sid uuid.UUID, _ trackedConn) bool { return sid == sessionID })
	return err
}

// RevokeUserSessions revokes a user's sessions (sparing except, if set) and
// closes their open connections.
func (s *Service) RevokeUserSessions(ctx context.Context, userID uuid.UUID, except *uuid.UUID) (int64, error) {
	n, err := s.store.RevokeUserSessions(ctx, userID, except)
	s.closeConns(func(sid uuid.UUID, c trackedConn) bool {
		return c.userID == userID && (except == nil || sid != *except)
	})
	return n, err
}

// SessionActive reports whether a session may still carry an open
// connection: not revoked, not expired, under the absolute cap, and its user
// still active. Revocations that don't go through this service (an admin
// disabling a user, for one) are caught by connections polling this.
// A database error answers true: a blip must not drop every console.
func (s *Service) SessionActive(ctx context.Context, sessionID uuid.UUID) bool {
	sess, err := s.store.GetSession(ctx, sessionID)
	if err != nil {
		return err != store.ErrNotFound
	}
	now := time.Now()
	if sess.RevokedAt != nil || !sess.ExpiresAt.After(now) || now.Sub(sess.CreatedAt) > sessionMaxLifetime {
		return false
	}
	user, err := s.store.GetUser(ctx, sess.UserID)
	if err != nil {
		return err != store.ErrNotFound
	}
	return user.Status == "active"
}
