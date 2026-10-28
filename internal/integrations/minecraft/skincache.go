package minecraft

import (
	"container/list"
	"strings"
	"sync"
	"time"
)

// SkinCache memoizes player skin PNGs so reopening an inventory does not
// re-hit Mojang. Bounded both ways: at most max entries (least recently used
// evicted first) and each entry expires after its TTL, so a skin changed on
// Mojang shows up within the hour and the cache cannot grow without limit
// however many uuids are asked for.
type SkinCache struct {
	mu    sync.Mutex
	max   int
	ll    *list.List
	items map[string]*list.Element
	now   func() time.Time
}

type skinEntry struct {
	key     string
	png     []byte
	expires time.Time
}

func NewSkinCache(max int) *SkinCache {
	return &SkinCache{max: max, ll: list.New(), items: map[string]*list.Element{}, now: time.Now}
}

// Get returns a live entry and marks it recently used.
func (c *SkinCache) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return nil, false
	}
	e := el.Value.(*skinEntry)
	if c.now().After(e.expires) {
		c.ll.Remove(el)
		delete(c.items, key)
		return nil, false
	}
	c.ll.MoveToFront(el)
	return e.png, true
}

// Put stores png for ttl, evicting the least recently used entry when full.
func (c *SkinCache) Put(key string, png []byte, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		e := el.Value.(*skinEntry)
		e.png, e.expires = png, c.now().Add(ttl)
		c.ll.MoveToFront(el)
		return
	}
	c.items[key] = c.ll.PushFront(&skinEntry{key: key, png: png, expires: c.now().Add(ttl)})
	for c.ll.Len() > c.max {
		oldest := c.ll.Back()
		c.ll.Remove(oldest)
		delete(c.items, oldest.Value.(*skinEntry).key)
	}
}

// Len reports the number of cached entries.
func (c *SkinCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ll.Len()
}

// NormalizePlayerUUID returns the 32-hex-digit lowercase form of a player
// uuid (dashes optional), or "" when s is not one. Only normalized uuids are
// looked up or cached, so arbitrary path strings never become cache keys.
func NormalizePlayerUUID(s string) string {
	compact := strings.ToLower(strings.ReplaceAll(s, "-", ""))
	if len(compact) != 32 {
		return ""
	}
	for _, r := range compact {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return ""
		}
	}
	return compact
}
