package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// TelegramChat is one Telegram chat linked to a HomeHub user. The bridge
// answers assistant messages in that chat under the linked user's
// permissions.
type TelegramChat struct {
	ChatID    int64
	UserID    uuid.UUID
	Label     string
	CreatedAt time.Time
}

func (s *Store) ListTelegramChats(ctx context.Context) ([]TelegramChat, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT chat_id, user_id, label, created_at FROM telegram_chats ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TelegramChat
	for rows.Next() {
		var c TelegramChat
		if err := rows.Scan(&c.ChatID, &c.UserID, &c.Label, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) GetTelegramChat(ctx context.Context, chatID int64) (*TelegramChat, error) {
	var c TelegramChat
	err := s.pool.QueryRow(ctx,
		`SELECT chat_id, user_id, label, created_at FROM telegram_chats WHERE chat_id=$1`, chatID).
		Scan(&c.ChatID, &c.UserID, &c.Label, &c.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) AddTelegramChat(ctx context.Context, chatID int64, userID uuid.UUID, label string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO telegram_chats (chat_id, user_id, label) VALUES ($1,$2,$3)
		 ON CONFLICT (chat_id) DO UPDATE SET user_id=EXCLUDED.user_id, label=EXCLUDED.label`,
		chatID, userID, label)
	return err
}

func (s *Store) DeleteTelegramChat(ctx context.Context, chatID int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM telegram_chats WHERE chat_id=$1`, chatID)
	return err
}
