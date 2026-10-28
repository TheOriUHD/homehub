package auth

import (
	"strings"
	"sync"
	"time"
)

// loginThrottle is the brute-force defence for password login. It used to be
// a per-account lock in the database, which let anyone who could reach the
// login page lock the administrator out by failing against their username.
// Now the hard lock is keyed on (username, client IP): a guesser locks only
// itself. Across all IPs, failures against one username add a growing delay
// to every attempt for it — this slows a distributed guesser without ever
// refusing the real owner, who just waits a moment longer.
//
// Combined with the per-IP token bucket (one IP can't spray many usernames),
// argon2id, and the 12-character minimum, that keeps online guessing far out
// of reach while removing the remote lock-out lever.
type loginThrottle struct {
	mu    sync.Mutex
	pairs map[string]*failureRecord // user|ip
	users map[string]*failureRecord // user
	now   func() time.Time

	lastScan time.Time
}

type failureRecord struct {
	count       int
	last        time.Time
	lockedUntil time.Time
}

const (
	// pairLockThreshold failures from one IP against one username lock that
	// pair; each further failure doubles the lock, up to lockoutMax.
	pairLockThreshold = lockoutThreshold

	// userDelayThreshold failures against one username (from anywhere)
	// before attempts for it start being slowed down.
	userDelayThreshold = 10
	userDelayStep      = 250 * time.Millisecond
	userDelayMax       = 3 * time.Second

	// failureWindow: a record with no new failure for this long is forgotten.
	failureWindow = 30 * time.Minute
)

func newLoginThrottle() *loginThrottle {
	return &loginThrottle{
		pairs: map[string]*failureRecord{},
		users: map[string]*failureRecord{},
		now:   time.Now,
	}
}

func throttleUser(username string) string { return strings.ToLower(strings.TrimSpace(username)) }

func pairKey(username, ip string) string { return throttleUser(username) + "|" + ip }

// check reports whether this IP is locked out of this username, and how long
// the caller should wait before verifying (the cross-IP slow-down).
func (t *loginThrottle) check(username, ip string) (lockedUntil time.Time, delay time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	t.gc(now)
	if p := t.pairs[pairKey(username, ip)]; p != nil && p.lockedUntil.After(now) {
		return p.lockedUntil, 0
	}
	if u := t.users[throttleUser(username)]; u != nil && u.count >= userDelayThreshold {
		delay = time.Duration(u.count-userDelayThreshold+1) * userDelayStep
		if delay > userDelayMax {
			delay = userDelayMax
		}
	}
	return time.Time{}, delay
}

// fail records a failed attempt and returns the pair's lock expiry, if this
// failure tripped (or extended) it.
func (t *loginThrottle) fail(username, ip string) (lockedUntil time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	bump := func(m map[string]*failureRecord, key string) *failureRecord {
		r := m[key]
		if r == nil || now.Sub(r.last) > failureWindow {
			r = &failureRecord{}
			m[key] = r
		}
		r.count++
		r.last = now
		return r
	}
	bump(t.users, throttleUser(username))
	p := bump(t.pairs, pairKey(username, ip))
	if p.count >= pairLockThreshold {
		lock := lockoutBase << (p.count - pairLockThreshold)
		if lock > lockoutMax || lock <= 0 {
			lock = lockoutMax
		}
		p.lockedUntil = now.Add(lock)
	}
	return p.lockedUntil
}

// succeed clears this IP's record for the username. The cross-IP counter is
// left to age out: one good login from the owner must not reset the
// slow-down an ongoing distributed attack has earned.
func (t *loginThrottle) succeed(username, ip string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.pairs, pairKey(username, ip))
}

// gc drops stale records so a spray of usernames/IPs can't grow memory
// without bound. Caller holds mu.
func (t *loginThrottle) gc(now time.Time) {
	if now.Sub(t.lastScan) < time.Minute {
		return
	}
	t.lastScan = now
	for _, m := range []map[string]*failureRecord{t.pairs, t.users} {
		for k, r := range m {
			if now.Sub(r.last) > failureWindow && !r.lockedUntil.After(now) {
				delete(m, k)
			}
		}
	}
}
