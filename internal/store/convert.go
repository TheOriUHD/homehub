package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Conversion is one converter batch. Items are loaded alongside it. All access
// is per-user (scoped by user_id) — there is no RBAC resource; this is personal
// data, like user_files.
type Conversion struct {
	ID           uuid.UUID  `db:"id"`
	UserID       uuid.UUID  `db:"user_id"`
	Status       string     `db:"status"`
	TargetFormat string     `db:"target_format"`
	CreatedAt    time.Time  `db:"created_at"`
	FinishedAt   *time.Time `db:"finished_at"`
	ExpiresAt    time.Time  `db:"expires_at"`
	Items        []ConversionItem
}

// ConversionItem is one file within a batch.
type ConversionItem struct {
	ID           uuid.UUID `db:"id"`
	ConversionID uuid.UUID `db:"conversion_id"`
	Position     int       `db:"position"`
	SourceName   string    `db:"source_name"`
	SourceBytes  int64     `db:"source_bytes"`
	SourceFormat string    `db:"source_format"`
	Category     string    `db:"category"`
	Status       string    `db:"status"`
	Error        string    `db:"error"`
	OutputName   string    `db:"output_name"`
	OutputBytes  int64     `db:"output_bytes"`
	DurationMs   int64     `db:"duration_ms"`
}

const (
	convCols = `id, user_id, status, target_format, created_at, finished_at, expires_at`
	itemCols = `id, conversion_id, position, source_name, source_bytes, source_format,
		category, status, error, output_name, output_bytes, duration_ms`
)

// CreateConversion inserts a batch and its items in one transaction. IDs are
// caller-generated (the API streams uploads to itemID-named files before the
// rows exist), so they are inserted verbatim.
func (s *Store) CreateConversion(ctx context.Context, c *Conversion) error {
	return s.WithTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			insert into conversions (id, user_id, status, target_format, expires_at)
			values ($1, $2, $3, $4, $5)`,
			c.ID, c.UserID, c.Status, c.TargetFormat, c.ExpiresAt); err != nil {
			return err
		}
		for i := range c.Items {
			it := &c.Items[i]
			it.ConversionID = c.ID
			if _, err := tx.Exec(ctx, `
				insert into conversion_items
					(id, conversion_id, position, source_name, source_bytes, source_format, category, status, error)
				values ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
				it.ID, c.ID, it.Position, it.SourceName, it.SourceBytes, it.SourceFormat,
				it.Category, it.Status, it.Error); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) loadItems(ctx context.Context, conversionID uuid.UUID) ([]ConversionItem, error) {
	items, err := scanMany[ConversionItem](s.pool.Query(ctx,
		`select `+itemCols+` from conversion_items where conversion_id = $1 order by position`, conversionID))
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []ConversionItem{}
	}
	return items, nil
}

// GetConversion loads one batch with items, scoped to the owner.
func (s *Store) GetConversion(ctx context.Context, userID, id uuid.UUID) (*Conversion, error) {
	c, err := scanOne[Conversion](s.pool.Query(ctx,
		`select `+convCols+` from conversions where id = $1 and user_id = $2`, id, userID))
	if err != nil {
		return nil, err
	}
	if c.Items, err = s.loadItems(ctx, c.ID); err != nil {
		return nil, err
	}
	return c, nil
}

// GetConversionByID loads a batch with items without user scoping — for the job
// worker, which runs on behalf of the system.
func (s *Store) GetConversionByID(ctx context.Context, id uuid.UUID) (*Conversion, error) {
	c, err := scanOne[Conversion](s.pool.Query(ctx,
		`select `+convCols+` from conversions where id = $1`, id))
	if err != nil {
		return nil, err
	}
	if c.Items, err = s.loadItems(ctx, c.ID); err != nil {
		return nil, err
	}
	return c, nil
}

// ListConversions returns the user's unexpired batches (with items), newest
// first.
func (s *Store) ListConversions(ctx context.Context, userID uuid.UUID) ([]Conversion, error) {
	convs, err := scanMany[Conversion](s.pool.Query(ctx,
		`select `+convCols+` from conversions
		 where user_id = $1 and expires_at > now()
		 order by created_at desc`, userID))
	if err != nil {
		return nil, err
	}
	for i := range convs {
		if convs[i].Items, err = s.loadItems(ctx, convs[i].ID); err != nil {
			return nil, err
		}
	}
	return convs, nil
}

// DeleteConversion removes a batch (cascading its items), scoped to the owner.
// Returns whether a row existed.
func (s *Store) DeleteConversion(ctx context.Context, userID, id uuid.UUID) (bool, error) {
	tag, err := s.pool.Exec(ctx, `delete from conversions where id = $1 and user_id = $2`, id, userID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// SetConversionStatus updates a batch's status, stamping finished_at for
// terminal states.
func (s *Store) SetConversionStatus(ctx context.Context, id uuid.UUID, status string, finished bool) error {
	if finished {
		_, err := s.pool.Exec(ctx,
			`update conversions set status = $2, finished_at = now() where id = $1`, id, status)
		return err
	}
	_, err := s.pool.Exec(ctx, `update conversions set status = $2 where id = $1`, id, status)
	return err
}

// SetItemConverting marks an item in-flight.
func (s *Store) SetItemConverting(ctx context.Context, itemID uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`update conversion_items set status = 'converting' where id = $1`, itemID)
	return err
}

// CompleteItem records a successful item output.
func (s *Store) CompleteItem(ctx context.Context, itemID uuid.UUID, outputName string, outputBytes, durationMs int64) error {
	_, err := s.pool.Exec(ctx, `
		update conversion_items
		set status = 'done', output_name = $2, output_bytes = $3, duration_ms = $4, error = ''
		where id = $1`, itemID, outputName, outputBytes, durationMs)
	return err
}

// FailItem records an item failure.
func (s *Store) FailItem(ctx context.Context, itemID uuid.UUID, msg string, durationMs int64) error {
	_, err := s.pool.Exec(ctx, `
		update conversion_items set status = 'failed', error = $2, duration_ms = $3 where id = $1`,
		itemID, msg, durationMs)
	return err
}

// SweepRunningConversions marks batches left 'running' by a crash as failed,
// along with their still-pending/converting items. Returns the count of
// batches swept.
func (s *Store) SweepRunningConversions(ctx context.Context) (int64, error) {
	if _, err := s.pool.Exec(ctx, `
		update conversion_items set status = 'failed', error = 'interrupted by restart'
		where status in ('pending', 'converting')
		  and conversion_id in (select id from conversions where status = 'running')`); err != nil {
		return 0, err
	}
	tag, err := s.pool.Exec(ctx, `
		update conversions set status = 'failed', finished_at = now() where status = 'running'`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// PurgeExpiredConversions deletes expired batches (cascading items) and returns
// their ids so the caller can remove their working directories.
func (s *Store) PurgeExpiredConversions(ctx context.Context) ([]uuid.UUID, error) {
	rows, err := s.pool.Query(ctx, `delete from conversions where expires_at < now() returning id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
