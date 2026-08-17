// Package metrics is history engine v1: raw samples recorded by integration
// drivers, rolled up into 5m and 1h tiers, queried by the graphs API. The
// full correlation/scrubbing UI arrives in a later phase on top of the same
// tables.
package metrics

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"homehub/internal/store"
)

// Sample is one metric observation for a resource.
type Sample struct {
	Resource string
	Metric   string
	Unit     string
	TS       time.Time
	Value    float64
}

// Retention defaults; admin-configurable in the history phase.
const (
	rawRetention  = 48 * time.Hour
	fiveMinKeep   = 14 * 24 * time.Hour
	hourKeep      = 400 * 24 * time.Hour
	watermarkKey  = "metrics_rollup_watermark"
	rollupLagSlop = 2 * time.Minute
)

type Service struct {
	store  *store.Store
	logger *slog.Logger

	mu     sync.Mutex
	series map[seriesKey]int64
}

type seriesKey struct{ resource, metric string }

func New(st *store.Store, logger *slog.Logger) *Service {
	return &Service{
		store:  st,
		logger: logger.With("component", "metrics"),
		series: map[seriesKey]int64{},
	}
}

// Record persists a batch of samples.
func (s *Service) Record(ctx context.Context, samples []Sample) error {
	rows := make([][]any, 0, len(samples))
	for _, sample := range samples {
		id, err := s.seriesID(ctx, sample.Resource, sample.Metric, sample.Unit)
		if err != nil {
			return err
		}
		ts := sample.TS
		if ts.IsZero() {
			ts = time.Now()
		}
		rows = append(rows, []any{id, ts, sample.Value})
	}
	return s.store.InsertMetricSamples(ctx, rows)
}

func (s *Service) seriesID(ctx context.Context, resource, metric, unit string) (int64, error) {
	key := seriesKey{resource, metric}
	s.mu.Lock()
	id, ok := s.series[key]
	s.mu.Unlock()
	if ok {
		return id, nil
	}
	id, err := s.store.EnsureMetricSeries(ctx, resource, metric, unit)
	if err != nil {
		return 0, err
	}
	s.mu.Lock()
	s.series[key] = id
	s.mu.Unlock()
	return id, nil
}

// Point is one graph point.
type Point struct {
	TS  time.Time `json:"ts"`
	Avg float64   `json:"avg"`
	Min float64   `json:"min"`
	Max float64   `json:"max"`
}

// Query returns points for a resource metric over [from, to], choosing the
// storage tier by span: raw under 6h, 5m rollups under 4 days, 1h beyond.
func (s *Service) Query(ctx context.Context, resource, metric string, from, to time.Time) ([]Point, error) {
	seriesID, err := s.store.MetricSeriesID(ctx, resource, metric)
	if err != nil {
		if err == store.ErrNotFound {
			return []Point{}, nil
		}
		return nil, err
	}
	span := to.Sub(from)
	var rows []store.MetricPoint
	switch {
	case span <= 6*time.Hour:
		rows, err = s.store.QueryMetricRaw(ctx, seriesID, from, to)
	case span <= 4*24*time.Hour:
		rows, err = s.store.QueryMetricRollup(ctx, seriesID, "5m", from, to)
	default:
		rows, err = s.store.QueryMetricRollup(ctx, seriesID, "1h", from, to)
	}
	if err != nil {
		return nil, err
	}
	points := make([]Point, 0, len(rows))
	for _, r := range rows {
		points = append(points, Point{TS: r.TS, Avg: r.Avg, Min: r.Min, Max: r.Max})
	}
	return points, nil
}

// RunMaintenance rolls completed windows into both tiers and applies
// retention. Registered as the "metrics.maintenance" job, scheduled every
// five minutes.
func (s *Service) RunMaintenance(ctx context.Context) error {
	var watermark time.Time
	found, err := s.store.GetSettingInto(ctx, watermarkKey, &watermark)
	if err != nil {
		return err
	}
	if !found || watermark.IsZero() {
		watermark = time.Now().Add(-rawRetention)
	}
	// Only roll windows that are fully closed (with slop for late writers).
	until := time.Now().Add(-rollupLagSlop).Truncate(5 * time.Minute)
	if until.After(watermark) {
		if err := s.store.RollupMetrics(ctx, "5m", 5*time.Minute, watermark, until); err != nil {
			return err
		}
		hourUntil := until.Truncate(time.Hour)
		hourSince := watermark.Truncate(time.Hour)
		if hourUntil.After(hourSince) {
			if err := s.store.RollupMetrics(ctx, "1h", time.Hour, hourSince, hourUntil); err != nil {
				return err
			}
		}
		if err := s.store.SetSetting(ctx, watermarkKey, until); err != nil {
			return err
		}
	}
	return s.store.PruneMetrics(ctx, rawRetention, fiveMinKeep, hourKeep)
}

// WirePayload shapes a sample batch for WebSocket delivery.
func WirePayload(samples []Sample) any {
	out := make([]map[string]any, 0, len(samples))
	for _, sample := range samples {
		out = append(out, map[string]any{
			"resource": sample.Resource,
			"metric":   sample.Metric,
			"ts":       sample.TS.UnixMilli(),
			"value":    sample.Value,
		})
	}
	return map[string]any{"samples": out}
}
