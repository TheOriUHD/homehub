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
	keyAppURL            = "app_url"
	keySessionTTL        = "session_ttl_hours"
	keyMetricsRawHours   = "metrics_raw_retention_hours"
	keyMetrics5mDays     = "metrics_5m_retention_days"
	keyMetrics1hDays     = "metrics_1h_retention_days"
	keyAssistantEnabled  = "assistant_enabled"
	keyAssistantProvider = "assistant_provider"
	keyAssistantModel    = "assistant_model"
	keyAssistantBaseURL  = "assistant_base_url"
	// The two Auto lanes for providers with no built-in catalog: a fast
	// "receptionist" model and one that can interpret.
	keyAssistantAutoBasic    = "assistant_auto_basic"
	keyAssistantAutoAdvanced = "assistant_auto_advanced"
	// The small model that writes push notifications. Separate from the chat
	// model on purpose: it runs unattended, once per condition, and should
	// never be the expensive one someone picked for a hard question.
	keyAssistantNotifyModel = "assistant_notify_model"
	keyTelegramEnabled      = "telegram_enabled"

	defaultSessionTTLHours = 720 // 30 days, rolling
	defaultRawHours        = 48
	default5mDays          = 14
	default1hDays          = 400
)

// MetricsRetention returns the three retention tiers.
func (s *Service) MetricsRetention(ctx context.Context) (raw, fiveMin, hour time.Duration) {
	rawHours, fiveMinDays, hourDays := defaultRawHours, default5mDays, default1hDays
	s.get(ctx, keyMetricsRawHours, &rawHours)
	s.get(ctx, keyMetrics5mDays, &fiveMinDays)
	s.get(ctx, keyMetrics1hDays, &hourDays)
	return time.Duration(rawHours) * time.Hour,
		time.Duration(fiveMinDays) * 24 * time.Hour,
		time.Duration(hourDays) * 24 * time.Hour
}

// MetricsRetentionValues returns the raw numbers for the settings API.
func (s *Service) MetricsRetentionValues(ctx context.Context) (rawHours, fiveMinDays, hourDays int) {
	rawHours, fiveMinDays, hourDays = defaultRawHours, default5mDays, default1hDays
	s.get(ctx, keyMetricsRawHours, &rawHours)
	s.get(ctx, keyMetrics5mDays, &fiveMinDays)
	s.get(ctx, keyMetrics1hDays, &hourDays)
	return
}

// SetMetricsRetention validates and stores the retention tiers.
func (s *Service) SetMetricsRetention(ctx context.Context, rawHours, fiveMinDays, hourDays int) error {
	if rawHours < 6 || rawHours > 720 {
		return fmt.Errorf("raw sample retention must be between 6 and 720 hours")
	}
	if fiveMinDays < 1 || fiveMinDays > 365 {
		return fmt.Errorf("5-minute rollup retention must be between 1 and 365 days")
	}
	if hourDays < 7 || hourDays > 3650 {
		return fmt.Errorf("hourly rollup retention must be between 7 and 3650 days")
	}
	for key, value := range map[string]int{
		keyMetricsRawHours: rawHours, keyMetrics5mDays: fiveMinDays, keyMetrics1hDays: hourDays,
	} {
		if err := s.store.SetSetting(ctx, key, value); err != nil {
			return err
		}
	}
	s.invalidate()
	return nil
}

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

// Assistant returns the assistant configuration (API key lives in the
// vault, not here). Defaults: disabled, Anthropic, the current Sonnet.
// Provider validity is the assistant registry's business, not ours.
func (s *Service) Assistant(ctx context.Context) (enabled bool, provider, model, baseURL string) {
	provider, model = "anthropic", "claude-sonnet-5"
	s.get(ctx, keyAssistantEnabled, &enabled)
	s.get(ctx, keyAssistantProvider, &provider)
	s.get(ctx, keyAssistantModel, &model)
	s.get(ctx, keyAssistantBaseURL, &baseURL)
	return
}

// AssistantAutoLanes returns the administrator-configured Auto lane models.
// Empty means "use the provider catalog's own lanes", which is how the cloud
// providers work; a local runtime has no catalog and needs both set before
// Auto can offer itself.
func (s *Service) AssistantAutoLanes(ctx context.Context) (basic, advanced string) {
	s.get(ctx, keyAssistantAutoBasic, &basic)
	s.get(ctx, keyAssistantAutoAdvanced, &advanced)
	return
}

// AssistantNotifyModel returns the model that writes push notifications.
// Empty means "the provider's basic Auto lane", which is the small one.
func (s *Service) AssistantNotifyModel(ctx context.Context) string {
	var model string
	s.get(ctx, keyAssistantNotifyModel, &model)
	return model
}

// SetAssistantNotifyModel stores it. Empty restores the default.
func (s *Service) SetAssistantNotifyModel(ctx context.Context, model string) error {
	if err := s.store.SetSetting(ctx, keyAssistantNotifyModel, strings.TrimSpace(model)); err != nil {
		return err
	}
	s.invalidate()
	return nil
}

// SetAssistantAutoLanes stores the Auto lane models.
func (s *Service) SetAssistantAutoLanes(ctx context.Context, basic, advanced string) error {
	if err := s.store.SetSetting(ctx, keyAssistantAutoBasic, strings.TrimSpace(basic)); err != nil {
		return err
	}
	if err := s.store.SetSetting(ctx, keyAssistantAutoAdvanced, strings.TrimSpace(advanced)); err != nil {
		return err
	}
	s.invalidate()
	return nil
}

// SetAssistant stores the assistant configuration.
func (s *Service) SetAssistant(ctx context.Context, enabled bool, provider, model, baseURL string) error {
	if strings.TrimSpace(model) == "" {
		return fmt.Errorf("the model name cannot be empty")
	}
	if err := s.store.SetSetting(ctx, keyAssistantEnabled, enabled); err != nil {
		return err
	}
	if err := s.store.SetSetting(ctx, keyAssistantProvider, provider); err != nil {
		return err
	}
	if err := s.store.SetSetting(ctx, keyAssistantModel, strings.TrimSpace(model)); err != nil {
		return err
	}
	if err := s.store.SetSetting(ctx, keyAssistantBaseURL, strings.TrimSpace(baseURL)); err != nil {
		return err
	}
	s.invalidate()
	return nil
}

// Telegram reports whether the Telegram bridge is enabled (the bot token
// lives in the vault, not here).
func (s *Service) Telegram(ctx context.Context) (enabled bool) {
	s.get(ctx, keyTelegramEnabled, &enabled)
	return
}

// SetTelegram stores the Telegram bridge switch.
func (s *Service) SetTelegram(ctx context.Context, enabled bool) error {
	return s.store.SetSetting(ctx, keyTelegramEnabled, enabled)
}
