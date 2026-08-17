// Package settings provides typed access to runtime application settings
// stored in the database — the configuration surface admins manage in-app.
package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"homehub/internal/store"
)

const (
	keyAppURL     = "app_url"
	keySessionTTL = "session_ttl_hours"

	defaultSessionTTLHours = 720 // 30 days, rolling
)

type Service struct {
	store *store.Store

	mu      sync.Mutex
	cache   map[string]json.RawMessage
	fetched time.Time
}

func New(st *store.Store) *Service {
	return &Service{store: st, cache: map[string]json.RawMessage{}}
}

// AppURL returns the configured external base URL ("" when unset).
func (s *Service) AppURL(ctx context.Context) string {
	var v string
	s.get(ctx, keyAppURL, &v)
	return strings.TrimRight(v, "/")
}

// SetAppURL validates and stores the external base URL.
func (s *Service) SetAppURL(ctx context.Context, raw string) error {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("app URL must be an absolute http(s) URL, e.g. https://homehub.example.com")
	}
	if err := s.store.SetSetting(ctx, keyAppURL, raw); err != nil {
		return err
	}
	s.invalidate()
	return nil
}

// SessionTTL returns the rolling session lifetime.
func (s *Service) SessionTTL(ctx context.Context) time.Duration {
	hours := defaultSessionTTLHours
	s.get(ctx, keySessionTTL, &hours)
	if hours < 1 {
		hours = defaultSessionTTLHours
	}
	return time.Duration(hours) * time.Hour
}

func (s *Service) SessionTTLHours(ctx context.Context) int {
	return int(s.SessionTTL(ctx) / time.Hour)
}

func (s *Service) SetSessionTTLHours(ctx context.Context, hours int) error {
	if hours < 1 || hours > 8760 {
		return fmt.Errorf("session lifetime must be between 1 and 8760 hours")
	}
	if err := s.store.SetSetting(ctx, keySessionTTL, hours); err != nil {
		return err
	}
	s.invalidate()
	return nil
}

func (s *Service) invalidate() {
	s.mu.Lock()
	s.fetched = time.Time{}
	s.mu.Unlock()
}

// get reads through a short cache; settings are read on hot paths (every
// session touch) but change rarely. On a miss or error dst keeps its default.
func (s *Service) get(ctx context.Context, key string, dst any) {
	s.mu.Lock()
	stale := time.Since(s.fetched) > 10*time.Second
	s.mu.Unlock()
	if stale {
		if all, err := s.store.AllSettings(ctx); err == nil {
			s.mu.Lock()
			s.cache = all
			s.fetched = time.Now()
			s.mu.Unlock()
		}
	}
	s.mu.Lock()
	raw, ok := s.cache[key]
	s.mu.Unlock()
	if ok {
		_ = json.Unmarshal(raw, dst)
	}
}
