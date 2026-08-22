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
	Route     *string   `db:"route"`
	CreatedAt time.Time `db:"created_at"`
}

const assistantCols = `id, parent_id, user_id, channel, conversation, role, content, tool_trace,
	model, input_tokens, output_tokens, route, created_at`

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
				m.model, m.input_tokens, m.output_tokens, m.route, m.created_at
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
	if usage != nil {
		model = usage.Model
		in, out = &usage.InputTokens, &usage.OutputTokens
		if usage.Route != "" {
			route = &usage.Route
		}
	}
	var id int64
	err := s.pool.QueryRow(ctx, `
		insert into assistant_messages
			(user_id, channel, conversation, role, content, tool_trace, parent_id,
			 model, input_tokens, output_tokens, route)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) returning id`,
		userID, channel, conversation, role, content, toolTrace, parentID, model, in, out, route).Scan(&id)
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

// PruneAssistantMessages garbage-collects old web-channel rows: a closed
// app's conversation id is never used again, so its rows are unreachable.
func (s *Store) PruneAssistantMessages(ctx context.Context, olderThan time.Duration) (int64, error) {
	tag, err := s.pool.Exec(ctx,
		`delete from assistant_messages where channel = $1 and created_at < now() - $2::interval`,
		AssistantChannelWeb, olderThan.String())
	return tag.RowsAffected(), err
}
