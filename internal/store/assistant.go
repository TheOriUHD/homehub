package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type AssistantMessage struct {
	ID        int64           `db:"id"`
	UserID    uuid.UUID       `db:"user_id"`
	Role      string          `db:"role"`
	Content   string          `db:"content"`
	ToolTrace json.RawMessage `db:"tool_trace"`
	CreatedAt time.Time       `db:"created_at"`
}

// ListAssistantMessages returns the newest `limit` messages in
// chronological order.
func (s *Store) ListAssistantMessages(ctx context.Context, userID uuid.UUID, limit int) ([]AssistantMessage, error) {
	return scanMany[AssistantMessage](s.pool.Query(ctx, `
		select * from (
			select id, user_id, role, content, tool_trace, created_at
			from assistant_messages where user_id = $1
			order by id desc limit $2
		) newest order by id asc`,
		userID, limit))
}

func (s *Store) AddAssistantMessage(ctx context.Context, userID uuid.UUID, role, content string, toolTrace json.RawMessage) error {
	_, err := s.pool.Exec(ctx, `
		insert into assistant_messages (user_id, role, content, tool_trace)
		values ($1, $2, $3, $4)`,
		userID, role, content, toolTrace)
	return err
}

func (s *Store) ClearAssistantMessages(ctx context.Context, userID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `delete from assistant_messages where user_id = $1`, userID)
	return err
}
