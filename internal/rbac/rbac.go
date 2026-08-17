// Package rbac implements resource-scoped permission evaluation.
//
// Resources form a tree addressed by slash-separated paths that integrations
// publish, e.g. "proxmox/8f3a…/guest/114" or "minecraft/2c1b…/server/lobby".
// App-level surfaces are resources too: "system/users", "system/roles",
// "system/settings", "system/audit", "system/health", "system/backups".
//
// A grant pairs a selector with a set of levels (view, control, configure,
// delete) and an effect (allow or deny). Selector semantics:
//
//   - a selector matches its node AND the node's entire subtree
//   - '*' matches exactly one path segment ("docker/*/container" = containers
//     on every docker host)
//   - therefore the selector "*" matches everything
//
// Evaluation is deny-by-default: a level is permitted only when at least one
// allow grant matches and no deny grant matches. A deny of "view" suppresses
// every level at that node, since nothing invisible may be acted on. Any allow
// grant implies "view" — a user who may control a thing can see it.
package rbac

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/TheOriUHD/homehub/internal/store"
)

type Level string

const (
	LevelView      Level = "view"
	LevelControl   Level = "control"
	LevelConfigure Level = "configure"
	LevelDelete    Level = "delete"
)

var AllLevels = []string{string(LevelView), string(LevelControl), string(LevelConfigure), string(LevelDelete)}

// Principal is the identity permissions are evaluated for. When ViewAsRole is
// set (admin preview), evaluation uses that role's grants exclusively.
type Principal struct {
	UserID     uuid.UUID
	ViewAsRole *uuid.UUID
}

// Engine evaluates permissions with a short-lived per-principal grant cache.
// The cache absorbs WebSocket fanout and list filtering; mutations to roles or
// grants must call Invalidate so changes apply promptly.
type Engine struct {
	store *store.Store
	ttl   time.Duration

	mu    sync.Mutex
	cache map[cacheKey]cacheEntry
}

type cacheKey struct {
	userID uuid.UUID
	roleID uuid.UUID // zero unless view-as
}

type cacheEntry struct {
	grants  []store.Grant
	expires time.Time
}

func NewEngine(st *store.Store) *Engine {
	return &Engine{store: st, ttl: 5 * time.Second, cache: map[cacheKey]cacheEntry{}}
}

// Can reports whether the principal holds the level on the resource.
func (e *Engine) Can(ctx context.Context, p Principal, resource string, level Level) (bool, error) {
	grants, err := e.grantsFor(ctx, p)
	if err != nil {
		return false, err
	}
	return Evaluate(grants, resource, level), nil
}

// CanView is shorthand for the visibility check used in list filtering.
func (e *Engine) CanView(ctx context.Context, p Principal, resource string) (bool, error) {
	return e.Can(ctx, p, resource, LevelView)
}

// Invalidate drops cached grants for one user (all view-as variants included
// via full flush of that user's keys).
func (e *Engine) Invalidate(userID uuid.UUID) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for k := range e.cache {
		if k.userID == userID {
			delete(e.cache, k)
		}
	}
}

// InvalidateAll drops the whole cache — called on any role/grant mutation,
// since role edits affect every member.
func (e *Engine) InvalidateAll() {
	e.mu.Lock()
	defer e.mu.Unlock()
	clear(e.cache)
}

func (e *Engine) grantsFor(ctx context.Context, p Principal) ([]store.Grant, error) {
	key := cacheKey{userID: p.UserID}
	if p.ViewAsRole != nil {
		key.roleID = *p.ViewAsRole
	}
	now := time.Now()

	e.mu.Lock()
	if entry, ok := e.cache[key]; ok && entry.expires.After(now) {
		e.mu.Unlock()
		return entry.grants, nil
	}
	e.mu.Unlock()

	var grants []store.Grant
	var err error
	if p.ViewAsRole != nil {
		grants, err = e.store.RoleGrantsOnly(ctx, *p.ViewAsRole)
	} else {
		grants, err = e.store.UserGrants(ctx, p.UserID)
	}
	if err != nil {
		return nil, err
	}

	e.mu.Lock()
	e.cache[key] = cacheEntry{grants: grants, expires: now.Add(e.ttl)}
	e.mu.Unlock()
	return grants, nil
}

// Evaluate applies the deny-by-default rules to a grant set.
func Evaluate(grants []store.Grant, resource string, level Level) bool {
	allowed := false
	for _, g := range grants {
		if !Match(g.Selector, resource) {
			continue
		}
		switch g.Effect {
		case "deny":
			// Deny of view suppresses everything; otherwise deny the listed levels.
			if slices.Contains(g.Levels, string(LevelView)) || slices.Contains(g.Levels, string(level)) {
				return false
			}
		default: // allow
			if level == LevelView || slices.Contains(g.Levels, string(level)) {
				allowed = true
			}
		}
	}
	return allowed
}

// Match reports whether a selector covers a resource path (node or subtree).
func Match(selector, resource string) bool {
	if selector == "" || resource == "" {
		return false
	}
	sel := strings.Split(selector, "/")
	res := strings.Split(resource, "/")
	if len(res) < len(sel) {
		return false
	}
	for i, seg := range sel {
		if seg == "*" {
			continue
		}
		if seg != res[i] {
			return false
		}
	}
	return true
}

// ValidSelector performs structural validation for admin input.
func ValidSelector(selector string) bool {
	if selector == "" || strings.HasPrefix(selector, "/") || strings.HasSuffix(selector, "/") {
		return false
	}
	for seg := range strings.SplitSeq(selector, "/") {
		if seg == "" {
			return false
		}
	}
	return true
}

// ValidLevels checks that every level is one of the four known levels.
func ValidLevels(levels []string) bool {
	if len(levels) == 0 {
		return false
	}
	for _, l := range levels {
		if !slices.Contains(AllLevels, l) {
			return false
		}
	}
	return true
}
