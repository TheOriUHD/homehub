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
	UserID       uuid.UUID       `db:"user_id"`
	Channel      string          `db:"channel"`
	Conversation string          `db:"conversation"`
	Role         string          `db:"role"`
	Content      string          `db:"content"`
	ToolTrace    json.RawMessage `db:"tool_trace"`
	CreatedAt    time.Time       `db:"created_at"`
}

// ListAssistantMessages returns the newest `limit` messages of one channel's
// conversation, in chronological order. The web channel scopes by the
// per-app-session conversation id; Telegram uses a single "" conversation.
func (s *Store) ListAssistantMessages(ctx context.Context, userID uuid.UUID, channel, conversation string, limit int) ([]AssistantMessage, error) {
	return scanMany[AssistantMessage](s.pool.Query(ctx, `
		select * from (
			select id, user_id, channel, conversation, role, content, tool_trace, created_at
			from assistant_messages where user_id = $1 and channel = $2 and conversation = $3
			order by id desc limit $4
		) newest order by id asc`,
		userID, channel, conversation, limit))
}

func (s *Store) AddAssistantMessage(ctx context.Context, userID uuid.UUID, channel, conversation, role, content string, toolTrace json.RawMessage) error {
	_, err := s.pool.Exec(ctx, `
		insert into assistant_messages (user_id, channel, conversation, role, content, tool_trace)
		values ($1, $2, $3, $4, $5, $6)`,
		userID, channel, conversation, role, content, toolTrace)
	return err
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
