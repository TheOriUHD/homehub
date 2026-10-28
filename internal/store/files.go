package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// UserFile is one workspace file: per-user storage behind the utilities
// area. Text files keep content in the row (editable, assistant-readable);
// binary files live on disk and the row is metadata only. Size is always
// set; Content only where the query loads it (and never for binaries).
type UserFile struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	Name      string
	Folder    string
	Content   string
	IsBinary  bool
	Mime      string
	Size      int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// UserFolder is one workspace folder; path is slash-joined ("a" or "a/b").
type UserFolder struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	Path      string
	CreatedAt time.Time
}

const fileMetaCols = `id, user_id, name, folder, is_binary, mime,
	CASE WHEN is_binary THEN blob_size ELSE octet_length(content) END,
	created_at, updated_at`

func scanFileMeta(row interface{ Scan(...any) error }, f *UserFile) error {
	return row.Scan(&f.ID, &f.UserID, &f.Name, &f.Folder, &f.IsBinary, &f.Mime, &f.Size, &f.CreatedAt, &f.UpdatedAt)
}

// UserFilesTotalBytes sums a user's workspace storage — blob sizes plus the
// octet length of text files — for per-user quota enforcement at upload time.
func (s *Store) UserFilesTotalBytes(ctx context.Context, userID uuid.UUID) (int64, error) {
	var total int64
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(CASE WHEN is_binary THEN blob_size ELSE octet_length(content) END), 0)
		 FROM user_files WHERE user_id=$1`, userID).Scan(&total)
	return total, err
}

// ListUserFiles returns the user's files newest-edited first, WITHOUT
// content (content loads per file).
func (s *Store) ListUserFiles(ctx context.Context, userID uuid.UUID) ([]UserFile, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+fileMetaCols+` FROM user_files WHERE user_id=$1 ORDER BY updated_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserFile
	for rows.Next() {
		var f UserFile
		if err := scanFileMeta(rows, &f); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// GetUserFile fetches one file with content, scoped to the owner. Binary
// files come back with empty Content — the blob lives on disk.
func (s *Store) GetUserFile(ctx context.Context, userID, id uuid.UUID) (*UserFile, error) {
	var f UserFile
	err := s.pool.QueryRow(ctx,
		`SELECT id, user_id, name, folder, is_binary, mime,
		        CASE WHEN is_binary THEN '' ELSE content END,
		        CASE WHEN is_binary THEN blob_size ELSE octet_length(content) END,
		        created_at, updated_at
		 FROM user_files WHERE id=$1 AND user_id=$2`, id, userID).
		Scan(&f.ID, &f.UserID, &f.Name, &f.Folder, &f.IsBinary, &f.Mime, &f.Content, &f.Size, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// GetUserFileByName fetches one text file with content by its exact name
// (the assistant's read tool addresses files by name, any folder).
func (s *Store) GetUserFileByName(ctx context.Context, userID uuid.UUID, name string) (*UserFile, error) {
	var f UserFile
	err := s.pool.QueryRow(ctx,
		`SELECT id, user_id, name, folder, is_binary, mime, content, octet_length(content), created_at, updated_at
		 FROM user_files WHERE user_id=$1 AND name=$2 AND NOT is_binary
		 ORDER BY updated_at DESC LIMIT 1`, userID, name).
		Scan(&f.ID, &f.UserID, &f.Name, &f.Folder, &f.IsBinary, &f.Mime, &f.Content, &f.Size, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// GetUserFileInFolder fetches metadata for an exact folder+name pair (used
// to give uploads collision-free names).
func (s *Store) GetUserFileInFolder(ctx context.Context, userID uuid.UUID, folder, name string) (*UserFile, error) {
	var f UserFile
	err := scanFileMeta(s.pool.QueryRow(ctx,
		`SELECT `+fileMetaCols+` FROM user_files WHERE user_id=$1 AND folder=$2 AND name=$3`,
		userID, folder, name), &f)
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// CreateUserFile inserts a new text file. A duplicate name in the same
// folder fails on the unique constraint.
func (s *Store) CreateUserFile(ctx context.Context, userID uuid.UUID, name, folder, content string) (*UserFile, error) {
	var f UserFile
	err := s.pool.QueryRow(ctx,
		`INSERT INTO user_files (user_id, name, folder, content) VALUES ($1,$2,$3,$4)
		 RETURNING id, user_id, name, folder, is_binary, mime, content, octet_length(content), created_at, updated_at`,
		userID, name, folder, content).
		Scan(&f.ID, &f.UserID, &f.Name, &f.Folder, &f.IsBinary, &f.Mime, &f.Content, &f.Size, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// CreateBinaryUserFile inserts the metadata row for a blob that the caller
// has written to disk under the given pre-generated id.
func (s *Store) CreateBinaryUserFile(ctx context.Context, id, userID uuid.UUID, name, folder, mime string, size int64) (*UserFile, error) {
	var f UserFile
	err := scanFileMeta(s.pool.QueryRow(ctx,
		`INSERT INTO user_files (id, user_id, name, folder, is_binary, mime, blob_size)
		 VALUES ($1,$2,$3,$4,TRUE,$5,$6)
		 RETURNING `+fileMetaCols, id, userID, name, folder, mime, size), &f)
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// UpdateUserFile renames and/or rewrites a text file, scoped to the owner.
func (s *Store) UpdateUserFile(ctx context.Context, userID, id uuid.UUID, name, content string) (*UserFile, error) {
	var f UserFile
	err := s.pool.QueryRow(ctx,
		`UPDATE user_files SET name=$3, content=$4, updated_at=now()
		 WHERE id=$1 AND user_id=$2 AND NOT is_binary
		 RETURNING id, user_id, name, folder, is_binary, mime, content, octet_length(content), created_at, updated_at`,
		id, userID, name, content).
		Scan(&f.ID, &f.UserID, &f.Name, &f.Folder, &f.IsBinary, &f.Mime, &f.Content, &f.Size, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// RenameUserFile renames any file (text or binary) without touching content.
func (s *Store) RenameUserFile(ctx context.Context, userID, id uuid.UUID, name string) (*UserFile, error) {
	var f UserFile
	err := scanFileMeta(s.pool.QueryRow(ctx,
		`UPDATE user_files SET name=$3, updated_at=now()
		 WHERE id=$1 AND user_id=$2 RETURNING `+fileMetaCols, id, userID, name), &f)
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// DeleteUserFile removes a file row, scoped to the owner. It reports the
// deleted row's binary flag so the caller can unlink the blob.
func (s *Store) DeleteUserFile(ctx context.Context, userID, id uuid.UUID) (found, wasBinary bool, err error) {
	err = s.pool.QueryRow(ctx,
		`DELETE FROM user_files WHERE id=$1 AND user_id=$2 RETURNING is_binary`, id, userID).Scan(&wasBinary)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, false, nil
		}
		return false, false, err
	}
	return true, wasBinary, nil
}

// ---- folders ---------------------------------------------------------------

// ListUserFolders returns the user's folders sorted by path.
func (s *Store) ListUserFolders(ctx context.Context, userID uuid.UUID) ([]UserFolder, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, user_id, path, created_at FROM user_folders WHERE user_id=$1 ORDER BY path`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserFolder
	for rows.Next() {
		var f UserFolder
		if err := rows.Scan(&f.ID, &f.UserID, &f.Path, &f.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// CreateUserFolder inserts one folder.
func (s *Store) CreateUserFolder(ctx context.Context, userID uuid.UUID, path string) (*UserFolder, error) {
	var f UserFolder
	err := s.pool.QueryRow(ctx,
		`INSERT INTO user_folders (user_id, path) VALUES ($1,$2)
		 RETURNING id, user_id, path, created_at`, userID, path).
		Scan(&f.ID, &f.UserID, &f.Path, &f.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// DeleteUserFolder removes a folder, its subfolders, and every file inside
// them, returning the ids of deleted BINARY files so blobs can be unlinked.
func (s *Store) DeleteUserFolder(ctx context.Context, userID, id uuid.UUID) (found bool, blobIDs []uuid.UUID, err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var path string
	if err := tx.QueryRow(ctx,
		`SELECT path FROM user_folders WHERE id=$1 AND user_id=$2`, id, userID).Scan(&path); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil, nil
		}
		return false, nil, err
	}

	rows, err := tx.Query(ctx,
		`DELETE FROM user_files WHERE user_id=$1 AND (folder=$2 OR folder LIKE $3 ESCAPE '\') RETURNING id, is_binary`,
		userID, path, likeUnder(path))
	if err != nil {
		return false, nil, err
	}
	for rows.Next() {
		var fid uuid.UUID
		var bin bool
		if err := rows.Scan(&fid, &bin); err != nil {
			rows.Close()
			return false, nil, err
		}
		if bin {
			blobIDs = append(blobIDs, fid)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return false, nil, err
	}

	if _, err := tx.Exec(ctx,
		`DELETE FROM user_folders WHERE user_id=$1 AND (path=$2 OR path LIKE $3 ESCAPE '\')`,
		userID, path, likeUnder(path)); err != nil {
		return false, nil, err
	}
	return true, blobIDs, tx.Commit(ctx)
}
