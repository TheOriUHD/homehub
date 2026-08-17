package auth

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// keyedLimiter applies a token-bucket limit per key (IP address, username).
// Idle buckets are dropped after an hour to bound memory.
type keyedLimiter struct {
	mu       sync.Mutex
	buckets  map[string]*bucketEntry
	limit    rate.Limit
	burst    int
	lastScan time.Time
}

type bucketEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

func newKeyedLimiter(perMinute float64, burst int) *keyedLimiter {
	return &keyedLimiter{
		buckets: map[string]*bucketEntry{},
		limit:   rate.Limit(perMinute / 60.0),
		burst:   burst,
	}
}

// Allow reports whether the key may proceed, consuming one token if so.
func (k *keyedLimiter) Allow(key string) bool {
	k.mu.Lock()
	defer k.mu.Unlock()

	now := time.Now()
	if now.Sub(k.lastScan) > time.Hour {
		for key, b := range k.buckets {
			if now.Sub(b.lastSeen) > time.Hour {
				delete(k.buckets, key)
			}
		}
		k.lastScan = now
	}

	b, ok := k.buckets[key]
	if !ok {
		b = &bucketEntry{limiter: rate.NewLimiter(k.limit, k.burst)}
		k.buckets[key] = b
	}
	b.lastSeen = now
	return b.limiter.Allow()
}
