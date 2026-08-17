package auth

import (
	"sync"
	"time"
)

// stateStore holds short-lived server-side state that must survive between
// two requests of one flow: pending MFA logins and WebAuthn challenges.
// In-memory is correct here — the app runs as a single instance and these
// tokens live for minutes; a restart simply asks the user to retry the flow.
type stateStore struct {
	mu      sync.Mutex
	entries map[string]stateEntry
}

type stateEntry struct {
	value   any
	expires time.Time
}

func newStateStore() *stateStore {
	s := &stateStore{entries: map[string]stateEntry{}}
	go func() {
		for range time.Tick(time.Minute) {
			s.mu.Lock()
			now := time.Now()
			for k, e := range s.entries {
				if e.expires.Before(now) {
					delete(s.entries, k)
				}
			}
			s.mu.Unlock()
		}
	}()
	return s
}

func (s *stateStore) put(key string, value any, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[key] = stateEntry{value: value, expires: time.Now().Add(ttl)}
}

// take retrieves and deletes in one step — flow tokens are single-use.
func (s *stateStore) take(key string) (any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[key]
	if !ok || e.expires.Before(time.Now()) {
		delete(s.entries, key)
		return nil, false
	}
	delete(s.entries, key)
	return e.value, true
}

// peek retrieves without consuming (pending MFA state survives a wrong code;
// the attempt counter inside the value guards brute force).
func (s *stateStore) peek(key string) (any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[key]
	if !ok || e.expires.Before(time.Now()) {
		delete(s.entries, key)
		return nil, false
	}
	return e.value, true
}

func (s *stateStore) delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, key)
}
