package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type AlertRule struct {
	ID               uuid.UUID  `db:"id"`
	Name             string     `db:"name"`
	Resource         string     `db:"resource"`
	Metric           string     `db:"metric"`
	Operator         string     `db:"operator"`
	Threshold        float64    `db:"threshold"`
	SustainedSeconds int        `db:"sustained_seconds"`
	Severity         string     `db:"severity"`
	Enabled          bool       `db:"enabled"`
	State            string     `db:"state"`
	StateChangedAt   time.Time  `db:"state_changed_at"`
	CreatedBy        *uuid.UUID `db:"created_by"`
	CreatedAt        time.Time  `db:"created_at"`
	UpdatedAt        time.Time  `db:"updated_at"`
}

const alertRuleCols = `id, name, resource, metric, operator, threshold, sustained_seconds,
	severity, enabled, state, state_changed_at, created_by, created_at, updated_at`

func (s *Store) ListAlertRules(ctx context.Context) ([]AlertRule, error) {
	return scanMany[AlertRule](s.pool.Query(ctx,
		`select `+alertRuleCols+` from alert_rules order by lower(name)`))
}

func (s *Store) EnabledAlertRules(ctx context.Context) ([]AlertRule, error) {
	return scanMany[AlertRule](s.pool.Query(ctx,
		`select `+alertRuleCols+` from alert_rules where enabled`))
}

func (s *Store) GetAlertRule(ctx context.Context, id uuid.UUID) (*AlertRule, error) {
	return scanOne[AlertRule](s.pool.Query(ctx,
		`select `+alertRuleCols+` from alert_rules where id = $1`, id))
}

func (s *Store) CreateAlertRule(ctx context.Context, rule *AlertRule) (*AlertRule, error) {
	return scanOne[AlertRule](s.pool.Query(ctx, `
		insert into alert_rules (name, resource, metric, operator, threshold, sustained_seconds, severity, enabled, created_by)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		returning `+alertRuleCols,
		rule.Name, rule.Resource, rule.Metric, rule.Operator, rule.Threshold,
		rule.SustainedSeconds, rule.Severity, rule.Enabled, rule.CreatedBy))
}

func (s *Store) UpdateAlertRule(ctx context.Context, rule *AlertRule) error {
	tag, err := s.pool.Exec(ctx, `
		update alert_rules
		set name = $2, resource = $3, metric = $4, operator = $5, threshold = $6,
		    sustained_seconds = $7, severity = $8, enabled = $9, updated_at = now()
		where id = $1`,
		rule.ID, rule.Name, rule.Resource, rule.Metric, rule.Operator, rule.Threshold,
		rule.SustainedSeconds, rule.Severity, rule.Enabled)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteAlertRule(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `delete from alert_rules where id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetAlertRuleState transitions rule state, returning true when it changed.
func (s *Store) SetAlertRuleState(ctx context.Context, id uuid.UUID, state string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		update alert_rules set state = $2, state_changed_at = now()
		where id = $1 and state <> $2`, id, state)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ---- notification channels -------------------------------------------------

type NotificationChannel struct {
	ID        uuid.UUID       `db:"id"`
	Type      string          `db:"type"`
	Name      string          `db:"name"`
	Config    json.RawMessage `db:"config"`
	Enabled   bool            `db:"enabled"`
	CreatedAt time.Time       `db:"created_at"`
	UpdatedAt time.Time       `db:"updated_at"`
}

const channelCols = `id, type, name, config, enabled, created_at, updated_at`

func (s *Store) ListChannels(ctx context.Context) ([]NotificationChannel, error) {
	return scanMany[NotificationChannel](s.pool.Query(ctx,
		`select `+channelCols+` from notification_channels order by lower(name)`))
}

func (s *Store) GetChannel(ctx context.Context, id uuid.UUID) (*NotificationChannel, error) {
	return scanOne[NotificationChannel](s.pool.Query(ctx,
		`select `+channelCols+` from notification_channels where id = $1`, id))
}

func (s *Store) CreateChannel(ctx context.Context, typ, name string, config any) (*NotificationChannel, error) {
	raw, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	return scanOne[NotificationChannel](s.pool.Query(ctx, `
		insert into notification_channels (type, name, config) values ($1, $2, $3)
		returning `+channelCols, typ, name, raw))
}

func (s *Store) UpdateChannel(ctx context.Context, id uuid.UUID, name string, config any, enabled bool) error {
	raw, err := json.Marshal(config)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `
		update notification_channels set name = $2, config = $3, enabled = $4, updated_at = now()
		where id = $1`, id, name, raw, enabled)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteChannel(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `delete from notification_channels where id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- notification preferences ----------------------------------------------

type NotificationPref struct {
	UserID     uuid.UUID   `db:"user_id"`
	EventType  string      `db:"event_type"`
	InApp      bool        `db:"inapp"`
	ChannelIDs []uuid.UUID `db:"channel_ids"`
}

func (s *Store) ListNotificationPrefs(ctx context.Context, userID uuid.UUID) ([]NotificationPref, error) {
	return scanMany[NotificationPref](s.pool.Query(ctx,
		`select user_id, event_type, inapp, channel_ids from notification_prefs where user_id = $1`, userID))
}

func (s *Store) GetNotificationPref(ctx context.Context, userID uuid.UUID, eventType string) (*NotificationPref, error) {
	return scanOne[NotificationPref](s.pool.Query(ctx,
		`select user_id, event_type, inapp, channel_ids from notification_prefs
		 where user_id = $1 and event_type = $2`, userID, eventType))
}

func (s *Store) UpsertNotificationPref(ctx context.Context, pref *NotificationPref) error {
	_, err := s.pool.Exec(ctx, `
		insert into notification_prefs (user_id, event_type, inapp, channel_ids)
		values ($1, $2, $3, $4)
		on conflict (user_id, event_type) do update
		set inapp = excluded.inapp, channel_ids = excluded.channel_ids`,
		pref.UserID, pref.EventType, pref.InApp, pref.ChannelIDs)
	return err
}

// ---- series catalog --------------------------------------------------------

type MetricSeriesInfo struct {
	Resource string `db:"resource"`
	Metric   string `db:"metric"`
	Unit     string `db:"unit"`
	// LastSeen is the newest sample or rollup this series has, nil when it
	// has neither. A series whose integration was removed keeps its catalog
	// row and stops receiving points; without this the picker offers it as if
	// it were live.
	LastSeen *time.Time `db:"last_seen"`
}

func (s *Store) ListMetricSeries(ctx context.Context) ([]MetricSeriesInfo, error) {
	// Rollups outlive raw samples under retention, so the newest of the two
	// is the real answer to "when did this last record anything".
	return scanMany[MetricSeriesInfo](s.pool.Query(ctx, `
		select s.resource, s.metric, s.unit,
		       greatest(
		           (select max(ts) from metric_samples where series_id = s.id),
		           (select max(ts) from metric_rollups where series_id = s.id)
		       ) as last_seen
		from metric_series s
		order by s.resource, s.metric`))
}

// RecentSamples returns raw samples for one series in a trailing window,
// oldest first — the alert evaluator's input.
func (s *Store) RecentSamples(ctx context.Context, resource, metric string, window time.Duration) ([]MetricPoint, error) {
	seriesID, err := s.MetricSeriesID(ctx, resource, metric)
	if err != nil {
		if err == ErrNotFound {
			return nil, nil
		}
		return nil, err
	}
	return scanMany[MetricPoint](s.pool.Query(ctx, `
		select ts, value as avg_value, value as min_value, value as max_value
		from metric_samples
		where series_id = $1 and ts > now() - $2::interval
		order by ts`, seriesID, window.String()))
}
