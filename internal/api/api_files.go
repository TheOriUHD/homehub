package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/TheOriUHD/homehub/internal/httpctx"
	"github.com/TheOriUHD/homehub/internal/store"
)

// Workspace files: per-user storage behind the utilities area — a text
// editor workspace that doubles as a simple personal cloud. Text files live
// in the DB (editable, assistant-readable); anything else is a blob under
// {DataDir}/workspace/{fileID}. No RBAC resource — files are strictly
// personal, scoped by the identity on every query.
const maxTextFileBytes = 1 << 20   // 1 MiB — the editor stays a text editor
const maxUploadBytes = 512 << 20   // 512 MiB per uploaded blob
const maxFileName = 200
const maxFolderPath = 400

// workspaceQuotaBytes caps each user's total workspace storage. Uploads that
// would push a user over it are rejected with 413, so no single account can
// fill the disk that also backs PostgreSQL and starve the whole control plane.
const workspaceQuotaBytes = 2 << 30 // 2 GiB per user

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

// validFolderPath accepts "" (root) or slash-joined segments where every
// segment is itself a valid file name.
func validFolderPath(path string) (string, bool) {
	path = strings.Trim(strings.TrimSpace(path), "/")
	if path == "" {
		return "", true
	}
	if len(path) > maxFolderPath {
		return "", false
	}
	segs := strings.Split(path, "/")
	for i, seg := range segs {
		clean, ok := validFileName(seg)
		if !ok {
			return "", false
		}
		segs[i] = clean
	}
	return strings.Join(segs, "/"), true
}

// blobPath is where a binary file's bytes live on disk.
func (s *Server) blobPath(id uuid.UUID) string {
	return filepath.Join(s.dataDir, "workspace", id.String())
}

func toAPIFile(f *store.UserFile, withContent bool) WorkspaceFile {
	out := WorkspaceFile{
		Id:        f.ID,
		Name:      f.Name,
		Folder:    f.Folder,
		IsBinary:  f.IsBinary,
		Mime:      f.Mime,
		Size:      f.Size,
		CreatedAt: f.CreatedAt,
		UpdatedAt: f.UpdatedAt,
	}
	if withContent && !f.IsBinary {
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
	folder := ""
	if req.Body.Folder != nil {
		folder, ok = validFolderPath(*req.Body.Folder)
		if !ok {
			return CreateFile400JSONResponse{BadRequestJSONResponse(errBody("bad_folder", "invalid folder path"))}, nil
		}
	}
	content := ""
	if req.Body.Content != nil {
		content = *req.Body.Content
	}
	if len(content) > maxTextFileBytes {
		return CreateFile400JSONResponse{BadRequestJSONResponse(errBody("too_large", "text files are capped at 1 MiB"))}, nil
	}
	f, err := s.store.CreateUserFile(ctx, id.User.ID, name, folder, content)
	if err != nil {
		if isUniqueViolation(err) {
			return CreateFile409JSONResponse(errBody("name_taken", "a file with that name already exists here")), nil
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
	if len(req.Body.Content) > maxTextFileBytes {
		return UpdateFile400JSONResponse{BadRequestJSONResponse(errBody("too_large", "text files are capped at 1 MiB"))}, nil
	}
	// Binary files rename through the same endpoint: their content cannot be
	// rewritten here, so only apply the name.
	existing, err := s.store.GetUserFile(ctx, id.User.ID, req.Id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return UpdateFile404JSONResponse{NotFoundJSONResponse(errBody("not_found", "no such file"))}, nil
		}
		return nil, err
	}
	var f *store.UserFile
	if existing.IsBinary {
		f, err = s.store.RenameUserFile(ctx, id.User.ID, req.Id, name)
	} else {
		f, err = s.store.UpdateUserFile(ctx, id.User.ID, req.Id, name, req.Body.Content)
	}
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return UpdateFile404JSONResponse{NotFoundJSONResponse(errBody("not_found", "no such file"))}, nil
		}
		if isUniqueViolation(err) {
			return UpdateFile409JSONResponse(errBody("name_taken", "a file with that name already exists here")), nil
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
	found, wasBinary, err := s.store.DeleteUserFile(ctx, id.User.ID, req.Id)
	if err != nil {
		return nil, err
	}
	if !found {
		return DeleteFile404JSONResponse{NotFoundJSONResponse(errBody("not_found", "no such file"))}, nil
	}
	if wasBinary {
		_ = os.Remove(s.blobPath(req.Id))
	}
	return DeleteFile204Response{}, nil
}

// ---- folders ---------------------------------------------------------------

func (s *Server) ListFolders(ctx context.Context, _ ListFoldersRequestObject) (ListFoldersResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	rows, err := s.store.ListUserFolders(ctx, id.User.ID)
	if err != nil {
		return nil, err
	}
	out := make(ListFolders200JSONResponse, 0, len(rows))
	for _, f := range rows {
		out = append(out, WorkspaceFolder{Id: f.ID, Path: f.Path, CreatedAt: f.CreatedAt})
	}
	return out, nil
}

func (s *Server) CreateFolder(ctx context.Context, req CreateFolderRequestObject) (CreateFolderResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	path, ok := validFolderPath(req.Body.Path)
	if !ok || path == "" {
		return CreateFolder400JSONResponse{BadRequestJSONResponse(errBody("bad_path", "invalid folder path"))}, nil
	}
	f, err := s.store.CreateUserFolder(ctx, id.User.ID, path)
	if err != nil {
		if isUniqueViolation(err) {
			return CreateFolder409JSONResponse(errBody("path_taken", "that folder already exists")), nil
		}
		return nil, err
	}
	return CreateFolder200JSONResponse(WorkspaceFolder{Id: f.ID, Path: f.Path, CreatedAt: f.CreatedAt}), nil
}

func (s *Server) DeleteFolder(ctx context.Context, req DeleteFolderRequestObject) (DeleteFolderResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	found, blobIDs, err := s.store.DeleteUserFolder(ctx, id.User.ID, req.Id)
	if err != nil {
		return nil, err
	}
	if !found {
		return DeleteFolder404JSONResponse{NotFoundJSONResponse(errBody("not_found", "no such folder"))}, nil
	}
	for _, bid := range blobIDs {
		_ = os.Remove(s.blobPath(bid))
	}
	return DeleteFolder204Response{}, nil
}

// ---- raw upload / download (mounted directly, not via the generated API) ---

// looksLikeText reports whether uploaded bytes can live as an editable text
// file: small enough, valid UTF-8, and free of NUL bytes.
func looksLikeText(b []byte) bool {
	return len(b) <= maxTextFileBytes && !strings.ContainsRune(string(b), 0) && utf8.Valid(b)
}

// collisionFreeName returns name, then "name (2)", "name (3)"… keeping the
// extension — used to land uploads without clobbering existing files.
func collisionFreeName(name string, taken func(string) bool) string {
	if !taken(name) {
		return name
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 2; i < 100; i++ {
		cand := fmt.Sprintf("%s (%d)%s", stem, i, ext)
		if !taken(cand) {
			return cand
		}
	}
	return fmt.Sprintf("%s-%s%s", stem, uuid.NewString()[:8], ext)
}

// writeJSON writes v as a JSON response.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// HandleFileUpload accepts multipart uploads of ANY file type into the
// workspace: POST /api/files/upload with fields "folder" and one or more
// "file" parts. Text-looking uploads become editable text files; everything
// else is stored as a blob on disk.
func (s *Server) HandleFileUpload(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		http.Error(w, `{"code":"unauthenticated","message":"sign in first"}`, http.StatusUnauthorized)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes+(1<<20))
	reader, err := r.MultipartReader()
	if err != nil {
		http.Error(w, `{"code":"bad_request","message":"multipart form expected"}`, http.StatusBadRequest)
		return
	}

	// Per-user storage quota: start from what the user already holds and add
	// each file as it lands, rejecting once the ceiling is crossed.
	usedBytes, err := s.store.UserFilesTotalBytes(ctx, id.User.ID)
	if err != nil {
		s.logger.Error("workspace quota lookup", "error", err)
		http.Error(w, `{"code":"upload_failed","message":"could not store the file"}`, http.StatusInternalServerError)
		return
	}

	folder := ""
	var uploaded []WorkspaceFile
	names := map[string]bool{}
	taken := func(n string) bool {
		if names[n] {
			return true
		}
		_, err := s.store.GetUserFileInFolder(ctx, id.User.ID, folder, n)
		return err == nil
	}

	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			http.Error(w, `{"code":"bad_request","message":"malformed multipart body"}`, http.StatusBadRequest)
			return
		}
		switch part.FormName() {
		case "folder":
			raw, _ := io.ReadAll(io.LimitReader(part, 1024))
			f, ok := validFolderPath(string(raw))
			if !ok {
				http.Error(w, `{"code":"bad_folder","message":"invalid folder path"}`, http.StatusBadRequest)
				return
			}
			folder = f
		case "file":
			name, ok := validFileName(filepath.Base(part.FileName()))
			if !ok {
				http.Error(w, `{"code":"bad_name","message":"invalid file name"}`, http.StatusBadRequest)
				return
			}
			name = collisionFreeName(name, taken)

			// Buffer the head to decide text vs binary, then stream the rest.
			head := make([]byte, maxTextFileBytes+1)
			n, rerr := io.ReadFull(part, head)
			head = head[:n]
			if rerr != nil && !errors.Is(rerr, io.EOF) && !errors.Is(rerr, io.ErrUnexpectedEOF) {
				http.Error(w, `{"code":"upload_failed","message":"read failed mid-upload"}`, http.StatusBadRequest)
				return
			}
			isText := errors.Is(rerr, io.EOF) || errors.Is(rerr, io.ErrUnexpectedEOF)
			if isText && looksLikeText(head) {
				if usedBytes+int64(len(head)) > workspaceQuotaBytes {
					http.Error(w, `{"code":"quota_exceeded","message":"you have reached your workspace storage limit"}`, http.StatusRequestEntityTooLarge)
					return
				}
				f, cerr := s.store.CreateUserFile(ctx, id.User.ID, name, folder, string(head))
				if cerr != nil {
					s.logger.Error("workspace upload (text)", "error", cerr)
					http.Error(w, `{"code":"upload_failed","message":"could not store the file"}`, http.StatusInternalServerError)
					return
				}
				usedBytes += int64(len(head))
				names[name] = true
				uploaded = append(uploaded, toAPIFile(f, false))
				continue
			}

			// Binary path: stream to disk under a fresh id, then record it.
			fid := uuid.New()
			dir := filepath.Join(s.dataDir, "workspace")
			if err := os.MkdirAll(dir, 0o700); err != nil {
				http.Error(w, `{"code":"upload_failed","message":"could not store the file"}`, http.StatusInternalServerError)
				return
			}
			dst, cerr := os.OpenFile(s.blobPath(fid), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if cerr != nil {
				http.Error(w, `{"code":"upload_failed","message":"could not store the file"}`, http.StatusInternalServerError)
				return
			}
			size := int64(0)
			if n, werr := dst.Write(head); werr != nil {
				dst.Close()
				_ = os.Remove(s.blobPath(fid))
				http.Error(w, `{"code":"upload_failed","message":"could not store the file"}`, http.StatusInternalServerError)
				return
			} else {
				size += int64(n)
			}
			copied, werr := io.Copy(dst, part)
			size += copied
			dst.Close()
			if werr != nil || size > maxUploadBytes {
				_ = os.Remove(s.blobPath(fid))
				code, msg := http.StatusInternalServerError, `{"code":"upload_failed","message":"could not store the file"}`
				if size > maxUploadBytes {
					code, msg = http.StatusBadRequest, `{"code":"too_large","message":"uploads are capped at 512 MiB"}`
				}
				http.Error(w, msg, code)
				return
			}
			if usedBytes+size > workspaceQuotaBytes {
				_ = os.Remove(s.blobPath(fid))
				http.Error(w, `{"code":"quota_exceeded","message":"you have reached your workspace storage limit"}`, http.StatusRequestEntityTooLarge)
				return
			}

			mimeType := part.Header.Get("Content-Type")
			if mimeType == "" || mimeType == "application/octet-stream" {
				if byExt := mime.TypeByExtension(filepath.Ext(name)); byExt != "" {
					mimeType = byExt
				} else {
					mimeType = "application/octet-stream"
				}
			}
			f, cerr := s.store.CreateBinaryUserFile(ctx, fid, id.User.ID, name, folder, mimeType, size)
			if cerr != nil {
				_ = os.Remove(s.blobPath(fid))
				s.logger.Error("workspace upload (binary)", "error", cerr)
				http.Error(w, `{"code":"upload_failed","message":"could not store the file"}`, http.StatusInternalServerError)
				return
			}
			usedBytes += size
			names[name] = true
			uploaded = append(uploaded, toAPIFile(f, false))
		}
	}

	writeJSON(w, http.StatusOK, uploaded)
}

// HandleFileDownload streams any workspace file verbatim:
// GET /api/files/{id}/download.
func (s *Server) HandleFileDownload(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		http.Error(w, "sign in first", http.StatusUnauthorized)
		return
	}
	fid, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	f, err := s.store.GetUserFile(ctx, id.User.ID, fid)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	mimeType := f.Mime
	if mimeType == "" {
		mimeType = "text/plain; charset=utf-8"
	}
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+strings.ReplaceAll(f.Name, `"`, "")+`"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")

	if !f.IsBinary {
		w.Header().Set("Content-Length", fmt.Sprint(len(f.Content)))
		_, _ = io.WriteString(w, f.Content)
		return
	}
	blob, err := os.Open(s.blobPath(f.ID))
	if err != nil {
		http.Error(w, "blob missing", http.StatusInternalServerError)
		return
	}
	defer blob.Close()
	http.ServeContent(w, r, f.Name, f.UpdatedAt, blob)
}
