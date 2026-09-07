package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// A proposal the agent is holding on a surface that cannot show buttons. The
// web assistant keeps its queued actions in the page and posts them back; a
// Telegram chat has nowhere to put them, so they wait here until the person
// answers /yes or /no.

// PendingActions is one live proposal: the actions themselves (opaque here —
// the assistant package owns their shape), and the message they hang off so
// the paused answer resumes on the right branch.
type PendingActions struct {
	ParentID  int64           `db:"parent_id"`
	Actions   json.RawMessage `db:"actions"`
	CreatedAt time.Time       `db:"created_at"`
	ExpiresAt time.Time       `db:"expires_at"`
}

// PutPendingActions records what the agent proposed, replacing whatever that
// user's channel was holding: the newest proposal is the only one that can be
// approved, so an older one can never be run by a later "yes".
func (s *Store) PutPendingActions(ctx context.Context, userID uuid.UUID, channel string,
	parentID int64, actions json.RawMessage, expiresAt time.Time) error {
	_, err := s.pool.Exec(ctx, `
		insert into assistant_pending_actions (user_id, channel, parent_id, actions, created_at, expires_at)
		values ($1, $2, $3, $4, now(), $5)
		on conflict (user_id, channel) do update
		   set parent_id = excluded.parent_id,
		       actions = excluded.actions,
		       created_at = excluded.created_at,
		       expires_at = excluded.expires_at`,
		userID, channel, parentID, actions, expiresAt)
	return err
}

// TakePendingActions claims the proposal: it returns and deletes it in one
// statement, so two approvals arriving together cannot both run it. Returns
// nil when nothing was waiting. An expired row is still returned — the caller
// says so rather than silently doing nothing — and is gone either way.
func (s *Store) TakePendingActions(ctx context.Context, userID uuid.UUID, channel string) (*PendingActions, error) {
	row, err := scanOne[PendingActions](s.pool.Query(ctx, `
		delete from assistant_pending_actions
		where user_id = $1 and channel = $2
		returning parent_id, actions, created_at, expires_at`, userID, channel))
	if errors.Is(err, ErrNotFound) || errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return row, err
}

// ClearPendingActions drops a proposal without running it. A new instruction
// supersedes the old one, so the previous proposal must stop being approvable
// the moment the person says something else.
func (s *Store) ClearPendingActions(ctx context.Context, userID uuid.UUID, channel string) error {
	_, err := s.pool.Exec(ctx,
		`delete from assistant_pending_actions where user_id = $1 and channel = $2`, userID, channel)
	return err
}
