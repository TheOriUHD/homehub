package signals

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"homehub/internal/store"
)

// Severity levels. "recovered" is the green class: a condition that cleared
// on its own and is worth a glance before dismissal.
const (
	SevInfo      = "info"
	SevWarning   = "warning"
	SevCritical  = "critical"
	SevRecovered = "recovered"
)

// Observation is one condition a collector currently sees (or one event it
// wants recorded). Key must be stable for the same condition across sweeps
// and namespaced within the collector.
type Observation struct {
	Key        string
	Severity   string
	Title      string
	Body       string
	SourceType string     // integration type id or "system"
	SourceID   *uuid.UUID // instance, when applicable
	SourceName string     // display name for the panel
	Detail     any        // deep payload (log excerpts, entity lists, run ids…)
	// AutoResolve marks a stateful condition: when a later sweep of the same
	// collector no longer reports the key, the signal flips to 'recovered'.
	// One-shot events (a failed run) leave this false and wait for dismissal.
	AutoResolve bool
	// Recur bumps the occurrence counter when the key is already live —
	// meaningful for repeating one-shot events.
	Recur bool
	// ResolveTitle/ResolveBody override the row text when auto-resolving.
	ResolveTitle string
	ResolveBody  string
}

// Collector sweeps one source and reports everything it currently sees.
// Returning an error skips reconciliation for the sweep (so a flaky upstream
// never mass-resolves its open signals).
type Collector interface {
	Name() string
	Interval() time.Duration
	Collect(ctx context.Context) ([]Observation, error)
}

// PublishFunc fans a signal event out to connected clients.
type PublishFunc func(topic string, data any)

// Service runs collectors on their intervals and reconciles their
// observations into the signals store.
type Service struct {
	store   *store.Store
	publish PublishFunc
	logger  *slog.Logger

	mu         sync.Mutex
	collectors []Collector
	lastRun    map[string]time.Time
	resolveMsg map[string][2]string // key → {title, body} to use on auto-resolve
	// suppressedMiss counts consecutive sweeps an acknowledged marker went
	// unobserved. Collectors swallow per-instance hiccups and return partial
	// lists, so a single missing sweep proves nothing — resolving an
	// acknowledgement is irreversible and must be debounced.
	suppressedMiss map[string]int
}

func NewService(st *store.Store, publish PublishFunc, logger *slog.Logger) *Service {
	return &Service{
		store:          st,
		publish:        publish,
		logger:         logger.With("component", "signals"),
		lastRun:        map[string]time.Time{},
		resolveMsg:     map[string][2]string{},
		suppressedMiss: map[string]int{},
	}
}

// Register adds a collector; call before the first sweep.
func (s *Service) Register(c Collector) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.collectors = append(s.collectors, c)
}

// Sweep runs every collector that is due. Wire it to a once-a-minute job.
func (s *Service) Sweep(ctx context.Context) {
	s.mu.Lock()
	due := make([]Collector, 0, len(s.collectors))
	now := time.Now()
	for _, c := range s.collectors {
		if last, ok := s.lastRun[c.Name()]; !ok || now.Sub(last) >= c.Interval() {
			s.lastRun[c.Name()] = now
			due = append(due, c)
		}
	}
	s.mu.Unlock()

	for _, c := range due {
		s.runCollector(ctx, c)
	}
}

func (s *Service) runCollector(ctx context.Context, c Collector) {
	cctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	obs, err := c.Collect(cctx)
	if err != nil {
		s.logger.Warn("collector failed", "collector", c.Name(), "error", err)
		return
	}

	live, err := s.store.ListLiveSignalKeys(ctx, c.Name())
	if err != nil {
		s.logger.Error("list live signals", "collector", c.Name(), "error", err)
		return
	}

	changed := false
	seen := map[string]bool{}
	for _, o := range obs {
		if o.Key == "" || o.Title == "" {
			continue
		}
		seen[o.Key] = true
		if s.reconcileObservation(ctx, c.Name(), o) {
			changed = true
		}
	}

	// Auto-resolve: live stateful conditions this sweep no longer reports.
	for key := range live {
		if seen[key] {
			continue
		}
		s.mu.Lock()
		msg := s.resolveMsg[key]
		delete(s.resolveMsg, key)
		s.mu.Unlock()
		// ListLiveSignalKeys only returns auto_resolve rows, so anything
		// here genuinely cleared; msg text is a best-effort override.
		if _, rerr := s.store.ResolveSignal(ctx, key, msg[0], msg[1]); rerr == nil {
			changed = true
		}
	}

	// The acknowledged-but-still-true set reconciles too: once a suppressed
	// condition clears, its marker is resolved so a future occurrence
	// surfaces fresh instead of staying muted forever. Resolution is
	// debounced over several consecutive misses because collectors swallow
	// per-instance errors into partial lists — one flaky sweep must never
	// silently discard an operator's acknowledgement.
	const suppressedMissThreshold = 3
	if suppressed, serr := s.store.ListSuppressedSignalKeys(ctx, c.Name()); serr == nil {
		s.mu.Lock()
		for key := range suppressed {
			if seen[key] {
				delete(s.suppressedMiss, key)
				continue
			}
			s.suppressedMiss[key]++
			if s.suppressedMiss[key] >= suppressedMissThreshold {
				delete(s.suppressedMiss, key)
				_ = s.store.ResolveSuppressedSignal(ctx, key)
			}
		}
		s.mu.Unlock()
	}

	if changed {
		s.notify(ctx)
	}
}

// severityRank orders severities for the escalation check.
func severityRank(sev string) int {
	switch sev {
	case SevCritical:
		return 3
	case SevWarning:
		return 2
	case SevInfo:
		return 1
	}
	return 0
}

// reconcileObservation records one observation, honouring acknowledgements:
// a condition the operator dismissed WHILE STILL TRUE stays hidden as long
// as it persists at the same (or lower) severity — it only resurfaces when
// it escalates, recurs as a fresh event, or clears and later returns.
// Returns whether anything operator-visible changed.
func (s *Service) reconcileObservation(ctx context.Context, collector string, o Observation) bool {
	var detail json.RawMessage
	if o.Detail != nil {
		if b, jerr := json.Marshal(o.Detail); jerr == nil {
			detail = b
		}
	}
	if o.AutoResolve {
		s.mu.Lock()
		s.resolveMsg[o.Key] = [2]string{o.ResolveTitle, o.ResolveBody}
		s.mu.Unlock()
	}

	prev, _ := s.store.GetLiveSignal(ctx, o.Key)
	if prev == nil && !o.Recur {
		if sup, _ := s.store.GetSuppressedSignal(ctx, o.Key); sup != nil {
			if severityRank(o.Severity) <= severityRank(sup.Severity) {
				// Acknowledged and unchanged-or-milder — keep it muted, but
				// keep the marker current so pruning can't unmute it early.
				// The stored severity stays at what the operator acknowledged:
				// lowering it would let a boundary flap (crit→warn→crit)
				// "escalate" past the lowered value and reopen the signal.
				_ = s.store.TouchSuppressedSignal(ctx, sup.ID, sup.Severity, o.Title, o.Body, detail)
				return false
			}
			// Escalated past what was acknowledged — resurface the same row.
			if _, rerr := s.store.ReopenSignal(ctx, sup.ID, o.Severity, o.Title, o.Body, detail); rerr == nil {
				return true
			}
		}
	}

	row, uerr := s.store.UpsertSignal(ctx, store.Signal{
		Key:         o.Key,
		Collector:   collector,
		SourceType:  o.SourceType,
		SourceID:    o.SourceID,
		SourceName:  o.SourceName,
		Severity:    o.Severity,
		AutoResolve: o.AutoResolve,
		Title:       o.Title,
		Body:        o.Body,
		Detail:      detail,
	}, o.Recur)
	if uerr != nil {
		s.logger.Error("upsert signal", "key", o.Key, "error", uerr)
		return false
	}
	return prev == nil || prev.Severity != row.Severity
}

// notify pushes fresh counts to every client; the panel and page refetch.
func (s *Service) notify(ctx context.Context) {
	counts, err := s.store.SignalCounts(ctx)
	if err != nil {
		return
	}
	s.publish("signals", map[string]any{"event": "changed", "counts": counts})
}

// Emit records an observation outside the sweep loop — for event-driven
// sources (a run just failed) that shouldn't wait for the next interval.
// It honours acknowledgements the same way the sweep does.
func (s *Service) Emit(ctx context.Context, collector string, o Observation) {
	if s.reconcileObservation(ctx, collector, o) {
		s.notify(ctx)
	}
}
