package api

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/TheOriUHD/homehub/internal/httpctx"
	"github.com/TheOriUHD/homehub/internal/store"
)

// Workspace files: per-user text storage behind the utilities editor. No
// RBAC resource — files are strictly personal, scoped by the identity on
// every query, and capped so the editor stays a text editor.
const maxFileBytes = 1 << 20 // 1 MiB
const maxFileName = 200

func validFileName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > maxFileName {
		return "", false
	}
	// A filename, not a path: no separators or control characters. Any
	// extension (or none) is welcome — downloads carry the name verbatim.
	if strings.ContainsAny(name, "/\\\x00") || name == "." || name == ".." {
		return "", false
	}
	for _, r := range name {
		if r < 0x20 {
			return "", false
		}
	}
	return name, true
}

func toAPIFile(f *store.UserFile, withContent bool) WorkspaceFile {
	out := WorkspaceFile{
		Id:        f.ID,
		Name:      f.Name,
		Size:      f.Size,
		CreatedAt: f.CreatedAt,
		UpdatedAt: f.UpdatedAt,
	}
	if withContent {
		content := f.Content
		out.Content = &content
	}
	return out
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func (s *Server) ListFiles(ctx context.Context, _ ListFilesRequestObject) (ListFilesResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	rows, err := s.store.ListUserFiles(ctx, id.User.ID)
	if err != nil {
		return nil, err
	}
	out := make(ListFiles200JSONResponse, 0, len(rows))
	for i := range rows {
		out = append(out, toAPIFile(&rows[i], false))
	}
	return out, nil
}

func (s *Server) CreateFile(ctx context.Context, req CreateFileRequestObject) (CreateFileResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	name, ok := validFileName(req.Body.Name)
	if !ok {
		return CreateFile400JSONResponse{BadRequestJSONResponse(errBody("bad_name", "file names cannot be empty, contain slashes, or exceed 200 characters"))}, nil
	}
	content := ""
	if req.Body.Content != nil {
		content = *req.Body.Content
	}
	if len(content) > maxFileBytes {
		return CreateFile400JSONResponse{BadRequestJSONResponse(errBody("too_large", "files are capped at 1 MiB"))}, nil
	}
	f, err := s.store.CreateUserFile(ctx, id.User.ID, name, content)
	if err != nil {
		if isUniqueViolation(err) {
			return CreateFile409JSONResponse(errBody("name_taken", "a file with that name already exists")), nil
		}
		return nil, err
	}
	return CreateFile200JSONResponse(toAPIFile(f, true)), nil
}

func (s *Server) GetFile(ctx context.Context, req GetFileRequestObject) (GetFileResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	f, err := s.store.GetUserFile(ctx, id.User.ID, req.Id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return GetFile404JSONResponse{NotFoundJSONResponse(errBody("not_found", "no such file"))}, nil
		}
		return nil, err
	}
	return GetFile200JSONResponse(toAPIFile(f, true)), nil
}

func (s *Server) UpdateFile(ctx context.Context, req UpdateFileRequestObject) (UpdateFileResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	name, ok := validFileName(req.Body.Name)
	if !ok {
		return UpdateFile400JSONResponse{BadRequestJSONResponse(errBody("bad_name", "file names cannot be empty, contain slashes, or exceed 200 characters"))}, nil
	}
	if len(req.Body.Content) > maxFileBytes {
		return UpdateFile400JSONResponse{BadRequestJSONResponse(errBody("too_large", "files are capped at 1 MiB"))}, nil
	}
	f, err := s.store.UpdateUserFile(ctx, id.User.ID, req.Id, name, req.Body.Content)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return UpdateFile404JSONResponse{NotFoundJSONResponse(errBody("not_found", "no such file"))}, nil
		}
		if isUniqueViolation(err) {
			return UpdateFile409JSONResponse(errBody("name_taken", "a file with that name already exists")), nil
		}
		return nil, err
	}
	return UpdateFile200JSONResponse(toAPIFile(f, true)), nil
}

func (s *Server) DeleteFile(ctx context.Context, req DeleteFileRequestObject) (DeleteFileResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	gone, err := s.store.DeleteUserFile(ctx, id.User.ID, req.Id)
	if err != nil {
		return nil, err
	}
	if !gone {
		return DeleteFile404JSONResponse{NotFoundJSONResponse(errBody("not_found", "no such file"))}, nil
	}
	return DeleteFile204Response{}, nil
}
