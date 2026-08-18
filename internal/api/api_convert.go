package api

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/TheOriUHD/homehub/internal/convert"
	"github.com/TheOriUHD/homehub/internal/httpctx"
	"github.com/TheOriUHD/homehub/internal/store"
)

// The universal file converter. Like workspace files, conversions are strictly
// personal data with no RBAC resource: every query is scoped by the caller's
// identity. External tools run privilege-dropped and sandboxed inside the
// convert service; this layer owns validation, streaming caps, and mapping.
const (
	maxConvertFiles      = 50
	maxConvertFileBytes  = int64(1) << 30 // 1 GiB
	maxConvertBatchBytes = int64(2) << 30 // 2 GiB
	maxConvertField      = 1 << 16        // 64 KiB for target/options fields
)

func (s *Server) ConvertFormats(ctx context.Context, _ ConvertFormatsRequestObject) (ConvertFormatsResponseObject, error) {
	if httpctx.IdentityFrom(ctx) == nil {
		return nil, ErrUnauthenticated
	}
	return ConvertFormats200JSONResponse(s.convertCatalog()), nil
}

func (s *Server) convertCatalog() ConvertCatalog {
	caps := s.convert.Caps()
	var cat ConvertCatalog
	cat.Tools.Ffmpeg = caps.FFmpeg
	cat.Tools.Imagemagick = caps.ImageMagick
	cat.Tools.Libraw = caps.Libraw
	cat.Tools.Pandoc = caps.Pandoc

	byCat := map[convert.Category][]string{}
	for _, f := range convert.Formats() {
		byCat[f.Category] = append(byCat[f.Category], f.ID)
	}
	for _, c := range convert.CategoryOrder {
		if len(byCat[c]) == 0 {
			continue
		}
		cat.Categories = append(cat.Categories, ConvertCategory{Id: string(c), Formats: byCat[c]})
	}

	for _, f := range convert.Formats() {
		outs := convert.Outputs(f.ID)
		cf := ConvertFormat{Id: f.ID, Category: string(f.Category), Label: f.Label, Extensions: f.Exts, Outputs: []ConvertOutput{}}
		for _, e := range outs {
			to, _ := convert.FormatByID(e.To)
			avail, reason := caps.Available(e)
			out := ConvertOutput{
				Format:        e.To,
				Label:         to.Label,
				Category:      string(to.Category),
				Available:     avail,
				CrossCategory: to.Category != f.Category,
			}
			if !avail && reason != "" {
				r := reason
				out.UnavailableReason = &r
			}
			if note := crossNote(f, to); note != "" {
				n := note
				out.Note = &n
			}
			if len(e.Options) > 0 {
				opts := make([]ConvertOption, 0, len(e.Options))
				for _, od := range e.Options {
					opts = append(opts, ConvertOption{Name: od.Name, Label: od.Label, Min: od.Min, Max: od.Max, Default: od.Default})
				}
				out.Options = &opts
			}
			cf.Outputs = append(cf.Outputs, out)
		}
		cat.Formats = append(cat.Formats, cf)
	}
	return cat
}

// crossNote is a short label for a cross-category edge, so the UI can render
// "MP3 — extract audio" rather than a bare tile.
func crossNote(from, to convert.Format) string {
	if from.Category == to.Category {
		return ""
	}
	switch {
	case from.Category == convert.CategoryAudio && to.ID == "mp4":
		return "black video"
	case from.Category == convert.CategoryVideo && to.Category == convert.CategoryAudio:
		return "extract audio"
	case from.Category == convert.CategoryVideo && (to.ID == "jpg" || to.ID == "png"):
		return "poster frame"
	case from.Category == convert.CategoryAnimation && to.Category == convert.CategoryVideo:
		return "to video"
	case from.Category == convert.CategoryAnimation && to.ID == "png":
		return "first frame"
	case from.Category == convert.CategoryTable && (to.ID == "md" || to.ID == "html"):
		return "as table"
	case from.Category == convert.CategoryImage && to.ID == "pdf":
		return "as document"
	case from.Category == convert.CategoryImage && to.ID == "gif":
		return "as gif"
	}
	return ""
}

func (s *Server) ListConversions(ctx context.Context, _ ListConversionsRequestObject) (ListConversionsResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	rows, err := s.store.ListConversions(ctx, id.User.ID)
	if err != nil {
		return nil, err
	}
	out := make(ListConversions200JSONResponse, 0, len(rows))
	for i := range rows {
		out = append(out, toAPIConvertBatch(&rows[i]))
	}
	return out, nil
}

func (s *Server) GetConversion(ctx context.Context, req GetConversionRequestObject) (GetConversionResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conv, err := s.store.GetConversion(ctx, id.User.ID, req.Id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, store.ErrNotFound) {
			return GetConversion404JSONResponse{notFoundErr("conversion")}, nil
		}
		return nil, err
	}
	return GetConversion200JSONResponse(toAPIConvertBatch(conv)), nil
}

func (s *Server) DeleteConversion(ctx context.Context, req DeleteConversionRequestObject) (DeleteConversionResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	gone, err := s.store.DeleteConversion(ctx, id.User.ID, req.Id)
	if err != nil {
		return nil, err
	}
	if !gone {
		return DeleteConversion404JSONResponse{notFoundErr("conversion")}, nil
	}
	s.convert.RemoveDir(req.Id)
	return DeleteConversion204Response{}, nil
}

// CreateConversion streams a multipart upload straight to disk (never buffering
// a whole file in memory), enforcing the file/batch size and count caps as it
// goes, detects each file's format server-side, and enqueues one batch job.
func (s *Server) CreateConversion(ctx context.Context, req CreateConversionRequestObject) (CreateConversionResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if req.Body == nil {
		return CreateConversion400JSONResponse{BadRequestJSONResponse(errBody("bad_request", "expected a multipart upload"))}, nil
	}

	convID := uuid.New()
	inDir, _, _, err := s.convert.PrepareDirs(convID)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			s.convert.RemoveDir(convID)
		}
	}()

	type upload struct {
		itemID  uuid.UUID
		name    string
		bytes   int64
		format  convert.Format
		ok      bool
		tmpPath string
	}

	var (
		target  string
		options convert.Options
		uploads []upload
		total   int64
	)

	// Bound TOTAL parts, not just file parts: otherwise a client can dribble an
	// endless stream of tiny form fields and pin this goroutine (slowloris).
	const maxParts = 200
	parts := 0
	for {
		part, perr := req.Body.NextPart()
		if errors.Is(perr, io.EOF) {
			break
		}
		parts++
		if parts > maxParts {
			return CreateConversion400JSONResponse{BadRequestJSONResponse(errBody("too_many_parts", "too many multipart sections"))}, nil
		}
		if perr != nil {
			return CreateConversion400JSONResponse{BadRequestJSONResponse(errBody("bad_request", "malformed multipart upload"))}, nil
		}

		if part.FileName() == "" {
			// A form field (target / options).
			val, rerr := io.ReadAll(io.LimitReader(part, maxConvertField))
			part.Close()
			if rerr != nil {
				continue
			}
			switch part.FormName() {
			case "target":
				target = strings.TrimSpace(string(val))
			case "options":
				_ = json.Unmarshal(val, &options)
			}
			continue
		}

		if len(uploads) >= maxConvertFiles {
			part.Close()
			return CreateConversion400JSONResponse{BadRequestJSONResponse(errBody("too_many_files", fmt.Sprintf("a batch is limited to %d files", maxConvertFiles)))}, nil
		}

		itemID := uuid.New()
		tmp := s.convert.InputPath(inDir, itemID, "") // {itemID}.bin while undetected
		limit := maxConvertFileBytes
		if rem := maxConvertBatchBytes - total; rem < limit {
			limit = rem
		}
		n, exceeded, werr := writeCapped(tmp, part, limit)
		part.Close()
		if werr != nil {
			_ = os.Remove(tmp)
			return nil, werr
		}
		if exceeded {
			_ = os.Remove(tmp)
			if total+n > maxConvertBatchBytes {
				return CreateConversion400JSONResponse{BadRequestJSONResponse(errBody("too_large", "the batch exceeds the 2 GiB limit"))}, nil
			}
			return CreateConversion400JSONResponse{BadRequestJSONResponse(errBody("too_large", "a file exceeds the 1 GiB limit"))}, nil
		}
		total += n

		u := upload{itemID: itemID, name: sanitizeUpload(part.FileName()), bytes: n, tmpPath: tmp}
		if f, ok := convert.Detect(tmp, u.name); ok {
			u.format = f
			u.ok = true
		}
		uploads = append(uploads, u)
	}

	if _, ok := convert.FormatByID(target); !ok {
		return CreateConversion400JSONResponse{BadRequestJSONResponse(errBody("bad_target", "unknown target format"))}, nil
	}
	if len(uploads) == 0 {
		return CreateConversion400JSONResponse{BadRequestJSONResponse(errBody("no_files", "no files were uploaded"))}, nil
	}

	// Resolve each upload against the matrix and the live tool probe, moving
	// convertible inputs into place and dropping the rest as failed items.
	conv := &store.Conversion{ID: convID, UserID: id.User.ID, TargetFormat: target}
	var anyPending bool
	var toolMissing string // set when an edge exists but its engine is absent
	for i, u := range uploads {
		item := store.ConversionItem{ID: u.itemID, Position: i, SourceName: u.name, SourceBytes: u.bytes}
		if !u.ok || !convert.IsInput(u.format.ID) {
			item.Status = "failed"
			item.Error = "unsupported file type"
			_ = os.Remove(u.tmpPath)
			conv.Items = append(conv.Items, item)
			continue
		}
		item.SourceFormat = u.format.ID
		item.Category = string(u.format.Category)
		_, exists, available, reason := s.convert.EdgeStatus(u.format.ID, target)
		switch {
		case !exists:
			item.Status = "failed"
			item.Error = fmt.Sprintf("cannot convert %s to %s", u.format.Label, target)
			_ = os.Remove(u.tmpPath)
		case !available:
			item.Status = "failed"
			if reason == "" {
				reason = "the required tool is not installed"
			}
			item.Error = reason
			toolMissing = reason
			_ = os.Remove(u.tmpPath)
		default:
			item.Status = "pending"
			anyPending = true
			if err := os.Rename(u.tmpPath, s.convert.InputPath(inDir, u.itemID, u.format.ID)); err != nil {
				item.Status = "failed"
				item.Error = "could not stage the upload"
			}
		}
		conv.Items = append(conv.Items, item)
	}

	// If nothing is convertible only because the engine is missing, that is a
	// tool_missing rejection rather than a silently all-failed batch.
	if !anyPending && toolMissing != "" {
		return CreateConversion400JSONResponse{BadRequestJSONResponse(errBody("tool_missing", toolMissing))}, nil
	}

	if err := s.convert.CreateBatch(ctx, conv, options); err != nil {
		return nil, err
	}
	committed = true // the batch owns the directory now; keep it

	return CreateConversion200JSONResponse(toAPIConvertBatch(conv)), nil
}

func (s *Server) DownloadConversion(ctx context.Context, req DownloadConversionRequestObject) (DownloadConversionResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conv, err := s.store.GetConversion(ctx, id.User.ID, req.Id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, store.ErrNotFound) {
			return DownloadConversion404JSONResponse{notFoundErr("conversion")}, nil
		}
		return nil, err
	}
	var done []store.ConversionItem
	for _, it := range conv.Items {
		if it.Status == "done" {
			done = append(done, it)
		}
	}
	if len(done) == 0 {
		return DownloadConversion404JSONResponse{notFoundErr("converted files")}, nil
	}

	pr, pw := io.Pipe()
	go func() {
		zw := zip.NewWriter(pw)
		seen := map[string]int{}
		var writeErr error
		for _, it := range done {
			path := s.convert.OutputPath(conv.ID, it.ID, conv.TargetFormat)
			f, oerr := os.Open(path)
			if oerr != nil {
				continue
			}
			w, cerr := zw.Create(uniqueName(seen, it.OutputName))
			if cerr != nil {
				f.Close()
				writeErr = cerr
				break
			}
			_, ferr := io.Copy(w, f)
			f.Close()
			if ferr != nil {
				writeErr = ferr
				break
			}
		}
		if writeErr == nil {
			writeErr = zw.Close()
		}
		_ = pw.CloseWithError(writeErr)
	}()

	name := "converted-" + conv.TargetFormat + ".zip"
	httpctx.Writer(ctx).Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	return DownloadConversion200ApplicationzipResponse{Body: pr}, nil
}

func (s *Server) DownloadConversionItem(ctx context.Context, req DownloadConversionItemRequestObject) (DownloadConversionItemResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conv, err := s.store.GetConversion(ctx, id.User.ID, req.Id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, store.ErrNotFound) {
			return DownloadConversionItem404JSONResponse{notFoundErr("conversion")}, nil
		}
		return nil, err
	}
	var item *store.ConversionItem
	for i := range conv.Items {
		if conv.Items[i].ID == req.ItemId {
			item = &conv.Items[i]
			break
		}
	}
	if item == nil || item.Status != "done" {
		return DownloadConversionItem404JSONResponse{notFoundErr("converted file")}, nil
	}
	f, err := os.Open(s.convert.OutputPath(conv.ID, item.ID, conv.TargetFormat))
	if err != nil {
		return DownloadConversionItem404JSONResponse{notFoundErr("converted file")}, nil
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	httpctx.Writer(ctx).Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": item.OutputName}))
	return DownloadConversionItem200ApplicationoctetStreamResponse{Body: f, ContentLength: fi.Size()}, nil
}

// ---- mapping & helpers -----------------------------------------------------

func toAPIConvertBatch(c *store.Conversion) ConvertBatch {
	items := make([]ConvertItem, 0, len(c.Items))
	for i := range c.Items {
		items = append(items, toAPIConvertItem(&c.Items[i]))
	}
	b := ConvertBatch{
		Id:           c.ID,
		Status:       ConvertBatchStatus(c.Status),
		TargetFormat: c.TargetFormat,
		CreatedAt:    c.CreatedAt,
		ExpiresAt:    c.ExpiresAt,
		Items:        items,
	}
	if c.FinishedAt != nil {
		b.FinishedAt = c.FinishedAt
	}
	return b
}

func toAPIConvertItem(it *store.ConversionItem) ConvertItem {
	out := ConvertItem{
		Id:           it.ID,
		SourceName:   it.SourceName,
		SourceBytes:  it.SourceBytes,
		SourceFormat: it.SourceFormat,
		Category:     it.Category,
		Status:       ConvertItemStatus(it.Status),
	}
	if it.Error != "" {
		e := it.Error
		out.Error = &e
	}
	if it.OutputName != "" {
		n := it.OutputName
		out.OutputName = &n
	}
	if it.OutputBytes > 0 {
		b := it.OutputBytes
		out.OutputBytes = &b
	}
	if it.DurationMs > 0 {
		d := it.DurationMs
		out.DurationMs = &d
	}
	return out
}

// writeCapped streams a part to dst, writing at most limit bytes. It reports
// whether the source had more than limit bytes (the cap was exceeded).
func writeCapped(dst string, src *multipart.Part, limit int64) (written int64, exceeded bool, err error) {
	f, err := os.Create(dst)
	if err != nil {
		return 0, false, err
	}
	defer f.Close()
	if limit < 0 {
		limit = 0
	}
	n, cerr := io.CopyN(f, src, limit+1)
	if errors.Is(cerr, io.EOF) {
		return n, n > limit, nil
	}
	if cerr != nil {
		return n, false, cerr
	}
	// Copied limit+1 bytes without EOF → there was more than the cap allowed.
	return n, true, nil
}

func sanitizeUpload(name string) string {
	name = strings.TrimSpace(name)
	// Keep only the base name; drop any path the browser may have included.
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == '"' || r == 0 {
			return '_'
		}
		return r
	}, name)
	if name == "" || name == "." || name == ".." {
		return "upload"
	}
	if len(name) > 200 {
		name = name[len(name)-200:]
	}
	return name
}

// uniqueName disambiguates repeated download names inside the zip.
func uniqueName(seen map[string]int, name string) string {
	if name == "" {
		name = "file"
	}
	seen[name]++
	if seen[name] == 1 {
		return name
	}
	dot := strings.LastIndex(name, ".")
	if dot <= 0 {
		return fmt.Sprintf("%s (%d)", name, seen[name]-1)
	}
	return fmt.Sprintf("%s (%d)%s", name[:dot], seen[name]-1, name[dot:])
}
