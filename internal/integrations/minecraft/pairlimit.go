package minecraft

import (
	"sync"
	"time"
)

// Pairing-code brute-force limits. A code is 6 digits (a million values),
// lives 15 minutes, and the companion endpoint is reachable without a
// session, so wrong guesses are budgeted three ways:
//
//   - per source IP: pairFailuresPerIP within pairWindow, then that IP is
//     refused until its oldest failure ages out;
//   - globally: pairFailuresGlobal within pairWindow across all IPs, which
//     is what holds against an attacker with many addresses;
//   - per code: every live code is withdrawn after pairFailuresPerCode wrong
//     guesses (store.RecordFailedMinecraftPairing).
//
// The per-code budget dominates: at most 10 guesses per code, so the odds
// of hitting one are 10 in a million per code issued. Pairing is a rare,
// hands-on admin step; a legitimate owner who trips the global limit just
// waits a few minutes and generates a fresh code.
const (
	pairWindow          = 10 * time.Minute
	pairFailuresPerIP   = 5
	pairFailuresGlobal  = 30
	pairFailuresPerCode = 10
)

// pairLimiter tracks recent failed pairing attempts. Only failures are
// recorded, and nothing is recorded while the global budget is exhausted, so
// its memory is bounded by pairFailuresGlobal entries per window.
type pairLimiter struct {
	mu     sync.Mutex
	now    func() time.Time
	byIP   map[string][]time.Time
	global []time.Time
}

func newPairLimiter() *pairLimiter {
	return &pairLimiter{now: time.Now, byIP: map[string][]time.Time{}}
}

func recent(ts []time.Time, cutoff time.Time) []time.Time {
	i := 0
	for i < len(ts) && !ts[i].After(cutoff) {
		i++
	}
	return ts[i:]
}

func (l *pairLimiter) prune() time.Time {
	now := l.now()
	cutoff := now.Add(-pairWindow)
	l.global = recent(l.global, cutoff)
	for ip, ts := range l.byIP {
		if ts = recent(ts, cutoff); len(ts) == 0 {
			delete(l.byIP, ip)
		} else {
			l.byIP[ip] = ts
		}
	}
	return now
}

// allow reports whether ip may try a code now.
func (l *pairLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prune()
	return len(l.global) < pairFailuresGlobal && len(l.byIP[ip]) < pairFailuresPerIP
}

// fail records a wrong code from ip.
func (l *pairLimiter) fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.prune()
	if len(l.global) >= pairFailuresGlobal {
		return
	}
	l.global = append(l.global, now)
	l.byIP[ip] = append(l.byIP[ip], now)
}
