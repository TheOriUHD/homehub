package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type AuditEntry struct {
	ID        int64           `db:"id"`
	TS        time.Time       `db:"ts"`
	ActorKind string          `db:"actor_kind"`
	ActorID   *uuid.UUID      `db:"actor_id"`
	ActorName string          `db:"actor_name"`
	SessionID *uuid.UUID      `db:"session_id"`
	Action    string          `db:"action"`
	Resource  string          `db:"resource"`
	Outcome   string          `db:"outcome"`
	IP        string          `db:"ip"`
	Detail    json.RawMessage `db:"detail"`
}

type AuditFilter struct {
	ActorID  *uuid.UUID
	Action   string
	Resource string
	Outcome  string
	From     *time.Time
	To       *time.Time
	BeforeID *int64
	Limit    int
}

func (s *Store) InsertAudit(ctx context.Context, e *AuditEntry) error {
	detail := e.Detail
	if detail == nil {
		detail = json.RawMessage(`{}`)
	}
	_, err := s.pool.Exec(ctx, `
		insert into audit_log (actor_kind, actor_id, actor_name, session_id, action, resource, outcome, ip, detail)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		e.ActorKind, e.ActorID, e.ActorName, e.SessionID, e.Action, e.Resource, e.Outcome, e.IP, detail)
	return err
}

// PruneAudit deletes entries older than the retention window. It is the only
// delete the audit log allows: the append-only trigger (migration 00042)
// refuses every update and delete unless the transaction sets
// homehub.audit_retention, which only this function does — and only for
// rows past retention, never newer ones.
func (s *Store) PruneAudit(ctx context.Context, olderThan time.Duration) (int64, error) {
	var n int64
	err := s.WithTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `select set_config('homehub.audit_retention', 'on', true)`); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx,
			`delete from audit_log where ts < now() - $1::interval`, olderThan.String())
		n = tag.RowsAffected()
		return err
	})
	return n, err
}

// ListAudit pages newest-first with a keyset cursor. Resource filtering is a
// prefix match so "proxmox/inst1" finds everything under that instance.
func (s *Store) ListAudit(ctx context.Context, f AuditFilter) ([]AuditEntry, error) {
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	return scanMany[AuditEntry](s.pool.Query(ctx, `
		select id, ts, actor_kind, actor_id, actor_name, session_id, action, resource, outcome, ip, detail
		from audit_log
		where ($1::uuid is null or actor_id = $1)
		  and ($2 = '' or action = $2)
		  and ($3 = '' or resource = $3 or resource like $9 escape '\')
		  and ($4 = '' or outcome = $4)
		  and ($5::timestamptz is null or ts >= $5)
		  and ($6::timestamptz is null or ts <= $6)
		  and ($7::bigint is null or id < $7)
		order by id desc
		limit $8`,
		f.ActorID, f.Action, f.Resource, f.Outcome, f.From, f.To, f.BeforeID, limit, likeUnder(f.Resource)))
}
