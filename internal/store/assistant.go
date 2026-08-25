package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Assistant channels. Web and Telegram keep fully separate threads — the same
// user talking on both surfaces never sees one bleed into the other.
const (
	AssistantChannelWeb      = "web"
	AssistantChannelTelegram = "telegram"
)

type AssistantMessage struct {
	ID           int64           `db:"id"`
	ParentID     *int64          `db:"parent_id"`
	UserID       uuid.UUID       `db:"user_id"`
	Channel      string          `db:"channel"`
	Conversation string          `db:"conversation"`
	Role         string          `db:"role"`
	Content      string          `db:"content"`
	ToolTrace    json.RawMessage `db:"tool_trace"`
	// What producing this message cost. Model is "" and the token counts are
	// nil on user messages and on rows written before usage was recorded.
	Model        string `db:"model"`
	InputTokens  *int64 `db:"input_tokens"`
	OutputTokens *int64 `db:"output_tokens"`
	// Route is the Auto rule that chose Model, nil when the user pinned one.
	// Kept so an answer can show what decided, not just what ran.
	Route *string `db:"route"`
	// DurationMs is the wall-clock time the turn took end to end.
	DurationMs *int64 `db:"duration_ms"`
	// CostUsd was priced when the turn ran, so cache-rate splits and any
	// later price change cannot distort it.
	CostUsd   *float64  `db:"cost_usd"`
	CreatedAt time.Time `db:"created_at"`
}

const assistantCols = `id, parent_id, user_id, channel, conversation, role, content, tool_trace,
	model, input_tokens, output_tokens, route, duration_ms, cost_usd, created_at`

// ListAssistantMessages returns the newest `limit` messages of one channel's
// conversation, in chronological order — EVERY branch, not one path. The
// reader picks a path through the tree; the model is given one via
// AssistantThread. The web channel scopes by the per-app-session conversation
// id; Telegram uses a single "" conversation.
func (s *Store) ListAssistantMessages(ctx context.Context, userID uuid.UUID, channel, conversation string, limit int) ([]AssistantMessage, error) {
	return scanMany[AssistantMessage](s.pool.Query(ctx, `
		select * from (
			select `+assistantCols+`
			from assistant_messages where user_id = $1 and channel = $2 and conversation = $3
			order by id desc limit $4
		) newest order by id asc`,
		userID, channel, conversation, limit))
}

// AssistantThread returns one path through the tree: the message `leafID` and
// its ancestors, oldest first, capped at `limit` nearest messages. This is
// what the model sees — siblings on other branches are invisible to it, so a
// retried answer never learns about the answer it replaced.
func (s *Store) AssistantThread(ctx context.Context, userID uuid.UUID, leafID int64, limit int) ([]AssistantMessage, error) {
	return scanMany[AssistantMessage](s.pool.Query(ctx, `
		with recursive chain as (
			select `+assistantCols+`
			from assistant_messages where id = $1 and user_id = $2
			union all
			select m.id, m.parent_id, m.user_id, m.channel, m.conversation, m.role, m.content, m.tool_trace,
				m.model, m.input_tokens, m.output_tokens, m.route, m.duration_ms, m.cost_usd, m.created_at
			from assistant_messages m join chain c on m.id = c.parent_id
		)
		select * from (select * from chain order by id desc limit $3) nearest order by id asc`,
		leafID, userID, limit))
}

// AssistantUsage is what one answer cost to produce.
type AssistantUsage struct {
	Model        string
	InputTokens  int64
	OutputTokens int64
	// Route names the Auto rule that picked Model, empty when the user did.
	Route string
	// DurationMs is how long the turn took, end to end.
	DurationMs int64
	// CostUsd is the turn priced at the time it ran; nil when the model has
	// no published price.
	CostUsd *float64
}

// AddAssistantMessage appends a message under `parentID` (nil for a thread's
// first message) and returns its id, which becomes the next message's parent.
// `usage` is nil for user messages.
func (s *Store) AddAssistantMessage(
	ctx context.Context,
	userID uuid.UUID,
	channel, conversation, role, content string,
	toolTrace json.RawMessage,
	parentID *int64,
	usage *AssistantUsage,
) (int64, error) {
	var model string
	var in, out *int64
	var route *string
	var duration *int64
	var cost *float64
	if usage != nil {
		model = usage.Model
		in, out = &usage.InputTokens, &usage.OutputTokens
		if usage.Route != "" {
			route = &usage.Route
		}
		if usage.DurationMs > 0 {
			ms := usage.DurationMs
			duration = &ms
		}
		cost = usage.CostUsd
	}
	var id int64
	err := s.pool.QueryRow(ctx, `
		insert into assistant_messages
			(user_id, channel, conversation, role, content, tool_trace, parent_id,
			 model, input_tokens, output_tokens, route, duration_ms, cost_usd)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13) returning id`,
		userID, channel, conversation, role, content, toolTrace, parentID,
		model, in, out, route, duration, cost).Scan(&id)
	return id, err
}

// GetAssistantMessage loads one message, scoped to its owner so an id from
// another user's conversation resolves to nothing.
func (s *Store) GetAssistantMessage(ctx context.Context, userID uuid.UUID, id int64) (*AssistantMessage, error) {
	return scanOne[AssistantMessage](s.pool.Query(ctx,
		`select `+assistantCols+` from assistant_messages where id = $1 and user_id = $2`, id, userID))
}

// ClearAssistantMessages wipes one channel's entire history for a user —
// every conversation, so "clear chat" leaves nothing recoverable behind.
func (s *Store) ClearAssistantMessages(ctx context.Context, userID uuid.UUID, channel string) error {
	_, err := s.pool.Exec(ctx, `delete from assistant_messages where user_id = $1 and channel = $2`, userID, channel)
	return err
}

// PruneAssistantMessages garbage-collects rows nothing will read again.
//
// Web conversations are per-app-session: once one has been quiet for
// olderThan, its id never comes back and the whole tree goes. The newest row
// decides, not each row's own age — parent_id cascades on delete, so pruning
// a conversation's root by its own timestamp would take every branch under
// it, including the one written this morning.
//
// Telegram is one endless thread per user, read only as a linear window
// (RunTurn), and alerts are appended to it around the clock. It keeps its
// newest telegramKeep rows. Whatever survives with a pruned parent is
// unhooked first, for the same cascade reason.
func (s *Store) PruneAssistantMessages(ctx context.Context, olderThan time.Duration, telegramKeep int) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	web, err := tx.Exec(ctx, `
		delete from assistant_messages m
		using (
			select user_id, conversation from assistant_messages
			where channel = $1
			group by user_id, conversation
			having max(created_at) < now() - $2::interval
		) quiet
		where m.channel = $1 and m.user_id = quiet.user_id and m.conversation = quiet.conversation`,
		AssistantChannelWeb, olderThan.String())
	if err != nil {
		return 0, err
	}
	const ranked = `
		select id, row_number() over (partition by user_id, conversation order by id desc) as rn
		from assistant_messages where channel = $1`
	if _, err := tx.Exec(ctx, `
		with ranked as (`+ranked+`)
		update assistant_messages set parent_id = null
		where parent_id in (select id from ranked where rn > $2)
		  and id in (select id from ranked where rn <= $2)`,
		AssistantChannelTelegram, telegramKeep); err != nil {
		return 0, err
	}
	telegram, err := tx.Exec(ctx, `
		with ranked as (`+ranked+`)
		delete from assistant_messages where id in (select id from ranked where rn > $2)`,
		AssistantChannelTelegram, telegramKeep)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return web.RowsAffected() + telegram.RowsAffected(), nil
}
