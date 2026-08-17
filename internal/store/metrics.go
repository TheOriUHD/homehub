package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// EnsureMetricSeries resolves (resource, metric) to a series id, creating the
// series when new.
func (s *Store) EnsureMetricSeries(ctx context.Context, resource, metric, unit string) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		insert into metric_series (resource, metric, unit) values ($1, $2, $3)
		on conflict (resource, metric) do update set unit = excluded.unit
		returning id`, resource, metric, unit).Scan(&id)
	return id, err
}

type MetricPoint struct {
	TS  time.Time `db:"ts"`
	Avg float64   `db:"avg_value"`
	Min float64   `db:"min_value"`
	Max float64   `db:"max_value"`
}

// InsertMetricSamples bulk-inserts raw samples.
func (s *Store) InsertMetricSamples(ctx context.Context, rows [][]any) error {
	_, err := s.pool.CopyFrom(ctx,
		pgx.Identifier{"metric_samples"},
		[]string{"series_id", "ts", "value"},
		pgx.CopyFromRows(rows))
	return err
}

func (s *Store) MetricSeriesID(ctx context.Context, resource, metric string) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx,
		`select id from metric_series where resource = $1 and metric = $2`, resource, metric).Scan(&id)
	if err != nil {
		return 0, notFound(err)
	}
	return id, nil
}

// QueryMetricRaw returns raw samples bucketed client-side (each sample is its
// own point; min=max=avg).
func (s *Store) QueryMetricRaw(ctx context.Context, seriesID int64, from, to time.Time) ([]MetricPoint, error) {
	return scanMany[MetricPoint](s.pool.Query(ctx, `
		select ts, value as avg_value, value as min_value, value as max_value
		from metric_samples
		where series_id = $1 and ts >= $2 and ts <= $3
		order by ts`, seriesID, from, to))
}

func (s *Store) QueryMetricRollup(ctx context.Context, seriesID int64, bucket string, from, to time.Time) ([]MetricPoint, error) {
	return scanMany[MetricPoint](s.pool.Query(ctx, `
		select ts, avg_value, min_value, max_value
		from metric_rollups
		where series_id = $1 and bucket = $2 and ts >= $3 and ts <= $4
		order by ts`, seriesID, bucket, from, to))
}

// RollupMetrics aggregates raw samples into one bucket tier for windows that
// closed between the watermark and now, returning the new watermark.
func (s *Store) RollupMetrics(ctx context.Context, bucket string, interval time.Duration, since, until time.Time) error {
	_, err := s.pool.Exec(ctx, `
		insert into metric_rollups (series_id, bucket, ts, avg_value, min_value, max_value, sample_count)
		select series_id, $1, date_bin($2::interval, ts, timestamptz 'epoch'),
		       avg(value), min(value), max(value), count(*)
		from metric_samples
		where ts >= $3 and ts < $4
		group by series_id, date_bin($2::interval, ts, timestamptz 'epoch')
		on conflict (series_id, bucket, ts) do update
		set avg_value = excluded.avg_value, min_value = excluded.min_value,
		    max_value = excluded.max_value, sample_count = excluded.sample_count`,
		bucket, interval.String(), since, until)
	return err
}

// PruneMetrics applies retention to raw samples and each rollup tier.
func (s *Store) PruneMetrics(ctx context.Context, rawKeep, fiveMinKeep, hourKeep time.Duration) error {
	if _, err := s.pool.Exec(ctx,
		`delete from metric_samples where ts < now() - $1::interval`, rawKeep.String()); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx,
		`delete from metric_rollups where bucket = '5m' and ts < now() - $1::interval`, fiveMinKeep.String()); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx,
		`delete from metric_rollups where bucket = '1h' and ts < now() - $1::interval`, hourKeep.String())
	return err
}
