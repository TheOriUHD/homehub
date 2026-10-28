package store

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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
//
// It returns the stored row, whose Code is what the person answers with.
func (s *Store) PutPendingActions(ctx context.Context, userID uuid.UUID, channel string,
	parentID int64, actions json.RawMessage, expiresAt time.Time) (*PendingActions, error) {
	return scanOne[PendingActions](s.pool.Query(ctx, `
		insert into assistant_pending_actions (user_id, channel, parent_id, actions, created_at, expires_at)
		values ($1, $2, $3, $4, clock_timestamp(), $5)
		on conflict (user_id, channel) do update
		   set parent_id = excluded.parent_id,
		       actions = excluded.actions,
		       created_at = excluded.created_at,
		       expires_at = excluded.expires_at
		returning parent_id, actions, created_at, expires_at`,
		userID, channel, parentID, actions, expiresAt))
}

// ErrPendingSuperseded: the approval named a proposal that is no longer the
// one being held — a newer one replaced it. Nothing was run or removed.
var ErrPendingSuperseded = errors.New("that proposal was replaced by a newer one")

// proposalAlphabet avoids look-alikes (no O/0/I/1) so a code read off a phone
// screen is typed back right.
const proposalAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// Code names this one proposal in four characters, so an approval can say
// which proposal it means. It is derived from the row itself — the message it
// hangs off and the instant it was stored — so every replacement gets a new
// code without the table needing a column for it. It identifies, it does not
// authorise: the approval still has to come from the linked chat.
func (p *PendingActions) Code() string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d|%d", p.ParentID, p.CreatedAt.UnixMicro())))
	out := make([]byte, 4)
	for i := range out {
		out[i] = proposalAlphabet[int(sum[i])%len(proposalAlphabet)]
	}
	return string(out)
}

// HeldPendingActions returns the proposal being held, or nil.
func (s *Store) HeldPendingActions(ctx context.Context, userID uuid.UUID, channel string) (*PendingActions, error) {
	row, err := scanOne[PendingActions](s.pool.Query(ctx, `
		select parent_id, actions, created_at, expires_at
		from assistant_pending_actions where user_id = $1 and channel = $2`, userID, channel))
	if errors.Is(err, ErrNotFound) || errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return row, err
}

// TakePendingActions claims the proposal named by code. Returns nil when
// nothing was waiting, and ErrPendingSuperseded — leaving the newer proposal
// in place — when the code names one that has since been replaced.
//
// The claim is one conditional delete keyed on the exact row the code was
// checked against, so two approvals arriving together cannot both run it, and
// a replacement landing between the check and the claim is not taken in its
// place. An expired row is still returned — the caller says so rather than
// silently doing nothing — and is gone either way.
func (s *Store) TakePendingActions(ctx context.Context, userID uuid.UUID, channel, code string) (*PendingActions, error) {
	held, err := s.HeldPendingActions(ctx, userID, channel)
	if err != nil || held == nil {
		return nil, err
	}
	if !strings.EqualFold(strings.TrimSpace(code), held.Code()) {
		return nil, ErrPendingSuperseded
	}
	row, err := scanOne[PendingActions](s.pool.Query(ctx, `
		delete from assistant_pending_actions
		where user_id = $1 and channel = $2 and parent_id = $3 and created_at = $4
		returning parent_id, actions, created_at, expires_at`,
		userID, channel, held.ParentID, held.CreatedAt))
	if errors.Is(err, ErrNotFound) || errors.Is(err, pgx.ErrNoRows) {
		// Claimed by a concurrent approval, or replaced in between.
		if now, _ := s.HeldPendingActions(ctx, userID, channel); now != nil {
			return nil, ErrPendingSuperseded
		}
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
