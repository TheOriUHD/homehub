package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// UserFile is one workspace file: per-user text storage behind the
// utilities area's editor, also readable by the assistant as context.
// Size is always set; Content only where the query loads it.
type UserFile struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	Name      string
	Content   string
	Size      int
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ListUserFiles returns the user's files newest-edited first, WITHOUT
// content (the list is a sidebar; content loads per file).
func (s *Store) ListUserFiles(ctx context.Context, userID uuid.UUID) ([]UserFile, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, user_id, name, octet_length(content), created_at, updated_at
		 FROM user_files WHERE user_id=$1 ORDER BY updated_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserFile
	for rows.Next() {
		var f UserFile
		if err := rows.Scan(&f.ID, &f.UserID, &f.Name, &f.Size, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// GetUserFile fetches one file with content, scoped to the owner.
func (s *Store) GetUserFile(ctx context.Context, userID, id uuid.UUID) (*UserFile, error) {
	var f UserFile
	err := s.pool.QueryRow(ctx,
		`SELECT id, user_id, name, content, created_at, updated_at
		 FROM user_files WHERE id=$1 AND user_id=$2`, id, userID).
		Scan(&f.ID, &f.UserID, &f.Name, &f.Content, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		return nil, err
	}
	f.Size = len(f.Content)
	return &f, nil
}

// GetUserFileByName fetches one file with content by its exact name (the
// assistant's read tool addresses files by name).
func (s *Store) GetUserFileByName(ctx context.Context, userID uuid.UUID, name string) (*UserFile, error) {
	var f UserFile
	err := s.pool.QueryRow(ctx,
		`SELECT id, user_id, name, content, created_at, updated_at
		 FROM user_files WHERE user_id=$1 AND name=$2`, userID, name).
		Scan(&f.ID, &f.UserID, &f.Name, &f.Content, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		return nil, err
	}
	f.Size = len(f.Content)
	return &f, nil
}

// CreateUserFile inserts a new file. A duplicate name for the same user
// fails on the unique constraint.
func (s *Store) CreateUserFile(ctx context.Context, userID uuid.UUID, name, content string) (*UserFile, error) {
	var f UserFile
	err := s.pool.QueryRow(ctx,
		`INSERT INTO user_files (user_id, name, content) VALUES ($1,$2,$3)
		 RETURNING id, user_id, name, content, created_at, updated_at`,
		userID, name, content).
		Scan(&f.ID, &f.UserID, &f.Name, &f.Content, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		return nil, err
	}
	f.Size = len(f.Content)
	return &f, nil
}

// UpdateUserFile renames and/or rewrites a file, scoped to the owner.
func (s *Store) UpdateUserFile(ctx context.Context, userID, id uuid.UUID, name, content string) (*UserFile, error) {
	var f UserFile
	err := s.pool.QueryRow(ctx,
		`UPDATE user_files SET name=$3, content=$4, updated_at=now()
		 WHERE id=$1 AND user_id=$2
		 RETURNING id, user_id, name, content, created_at, updated_at`,
		id, userID, name, content).
		Scan(&f.ID, &f.UserID, &f.Name, &f.Content, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		return nil, err
	}
	f.Size = len(f.Content)
	return &f, nil
}

// DeleteUserFile removes a file, scoped to the owner.
func (s *Store) DeleteUserFile(ctx context.Context, userID, id uuid.UUID) (bool, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM user_files WHERE id=$1 AND user_id=$2`, id, userID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}
