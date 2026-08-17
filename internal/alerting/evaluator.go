package alerting

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"homehub/internal/store"
)

// Notifier delivers alert transitions to the users who can see the resource.
type Notifier interface {
	NotifyResourceViewers(ctx context.Context, resource string,
		canView func(ctx context.Context, userID uuid.UUID) bool,
		severity, title, body, eventType string)
}

// Evaluator drives rule evaluation; registered as the "alerts.evaluate" job
// on a one-minute schedule.
type Evaluator struct {
	store    *store.Store
	notifier Notifier
	canView  func(ctx context.Context, userID uuid.UUID, resource string) bool
	logger   *slog.Logger
}

func NewEvaluator(st *store.Store, notifier Notifier,
	canView func(ctx context.Context, userID uuid.UUID, resource string) bool,
	logger *slog.Logger) *Evaluator {
	return &Evaluator{store: st, notifier: notifier, canView: canView, logger: logger.With("component", "alerts")}
}

// Run evaluates every enabled rule once.
func (e *Evaluator) Run(ctx context.Context) error {
	rules, err := e.store.EnabledAlertRules(ctx)
	if err != nil {
		return err
	}
	for _, rule := range rules {
		if err := e.evaluate(ctx, rule); err != nil {
			e.logger.Error("rule evaluation failed", "rule", rule.Name, "error", err)
		}
	}
	return nil
}

func (e *Evaluator) evaluate(ctx context.Context, rule store.AlertRule) error {
	sustained := time.Duration(rule.SustainedSeconds) * time.Second
	// Fetch a bit more than the window so coverage can be judged.
	samples, err := e.store.RecentSamples(ctx, rule.Resource, rule.Metric, sustained+2*time.Minute)
	if err != nil {
		return err
	}

	windowStart := time.Now().Add(-sustained)
	inWindow := samples[:0:0]
	for _, sample := range samples {
		if sample.TS.After(windowStart) {
			inWindow = append(inWindow, sample)
		}
	}

	// Insufficient or stale data cannot assert either state.
	state := "unknown"
	if len(inWindow) >= 2 && time.Since(inWindow[len(inWindow)-1].TS) < 2*time.Minute {
		violating := true
		for _, sample := range inWindow {
			if !violates(rule, sample.Avg) {
				violating = false
				break
			}
		}
		if violating {
			state = "firing"
		} else {
			state = "ok"
		}
	}

	previous := rule.State
	changed, err := e.store.SetAlertRuleState(ctx, rule.ID, state)
	if err != nil || !changed {
		return err
	}

	canView := func(ctx context.Context, userID uuid.UUID) bool {
		return e.canView(ctx, userID, rule.Resource)
	}
	switch {
	case state == "firing":
		e.notifier.NotifyResourceViewers(ctx, rule.Resource, canView,
			rule.Severity,
			fmt.Sprintf("Alert: %s", rule.Name),
			fmt.Sprintf("%s on %s has been %s %.4g for over %s",
				rule.Metric, rule.Resource,
				describeOperator(rule.Operator), rule.Threshold, sustained),
			"alert.firing")
	case state == "ok" && previous == "firing":
		e.notifier.NotifyResourceViewers(ctx, rule.Resource, canView,
			"success",
			fmt.Sprintf("Resolved: %s", rule.Name),
			fmt.Sprintf("%s on %s is back within bounds", rule.Metric, rule.Resource),
			"alert.resolved")
	}
	return nil
}

func violates(rule store.AlertRule, value float64) bool {
	if rule.Operator == "lt" {
		return value < rule.Threshold
	}
	return value > rule.Threshold
}

func describeOperator(op string) string {
	if op == "lt" {
		return "below"
	}
	return "above"
}
