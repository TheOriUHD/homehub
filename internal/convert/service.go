package convert

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/TheOriUHD/homehub/internal/store"
)

// Retention is how long a batch (rows + working directory) lives after
// creation before the cleanup job reaps it.
const Retention = 24 * time.Hour

// JobKindBatch and JobKindCleanup are the two job kinds this service registers.
const (
	JobKindBatch   = "convert.batch"
	JobKindCleanup = "convert.cleanup"
)

// PublishFunc delivers a progress event to one user's WebSocket connections.
type PublishFunc func(userID uuid.UUID, event string, data any)

// EnqueueFunc enqueues the single batch job (jobs.EnqueueOnce — max attempts 1).
type EnqueueFunc func(ctx context.Context, kind string, payload any) error

// Service is the converter's coordinator: it owns the on-disk working area,
// the capability probe cache, the privilege-dropping runner, and the batch and
// cleanup job handlers.
type Service struct {
	store   *store.Store
	dataDir string
	root    string
	runner  *Runner
	caps    *capsCache
	logger  *slog.Logger

	publish PublishFunc
	enqueue EnqueueFunc
}

// NewService constructs the service and prepares the working root. drop (run
// tools privilege-dropped as nobody) is enabled only when the process is root,
// since that is what lets it both chown the working dirs and setuid.
func NewService(st *store.Store, dataDir string, logger *slog.Logger, publish PublishFunc, enqueue EnqueueFunc) *Service {
	root := filepath.Join(dataDir, "convert")
	drop := os.Geteuid() == 0
	s := &Service{
		store:   st,
		dataDir: dataDir,
		root:    root,
		runner:  NewRunner(drop, logger),
		caps:    &capsCache{},
		logger:  logger.With("component", "convert"),
		publish: publish,
		enqueue: enqueue,
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		s.logger.Error("create convert root", "error", err)
	}
	if !s.runner.drop {
		s.logger.Warn("conversions run without privilege drop (not root, or setpriv missing)")
	}
	return s
}

// Caps returns the (cached, ~5-min) capability probe.
func (s *Service) Caps() *Caps {
	c := s.caps.get()
	c.SandboxBroken = s.runner.SandboxBroken()
	return c
}

// EdgeStatus resolves a (from,to) conversion to its edge, whether it exists and
// whether it is currently runnable.
func (s *Service) EdgeStatus(from, to string) (e Edge, exists, available bool, reason string) {
	e, exists = EdgeFor(from, to)
	if !exists {
		return Edge{}, false, false, ""
	}
	available, reason = s.Caps().Available(e)
	return e, true, available, reason
}

// ---- on-disk layout --------------------------------------------------------

func (s *Service) batchDir(id uuid.UUID) string { return filepath.Join(s.root, id.String()) }

// PrepareDirs creates in/tmp/out for a batch. When privilege drop is active the
// tree is handed to the sandbox uid so the dropped tools can read inputs and
// write outputs; root retains access regardless.
func (s *Service) PrepareDirs(id uuid.UUID) (inDir, tmpDir, outDir string, err error) {
	base := s.batchDir(id)
	inDir, tmpDir, outDir = filepath.Join(base, "in"), filepath.Join(base, "tmp"), filepath.Join(base, "out")
	for _, d := range []string{base, inDir, tmpDir, outDir} {
		if err = os.MkdirAll(d, 0o755); err != nil {
			return "", "", "", err
		}
	}
	if s.runner.drop {
		for _, d := range []string{base, inDir, tmpDir, outDir} {
			if err := os.Chown(d, dropUID, dropGID); err != nil {
				s.logger.Warn("chown batch dir", "dir", d, "error", err)
			}
			_ = os.Chmod(d, 0o700)
		}
	}
	return inDir, tmpDir, outDir, nil
}

// RemoveDir deletes a batch's whole working directory.
func (s *Service) RemoveDir(id uuid.UUID) {
	if err := os.RemoveAll(s.batchDir(id)); err != nil {
		s.logger.Warn("remove batch dir", "batch", id, "error", err)
	}
}

// InputPath is the server-generated path an item's upload is written to (and
// read back from by the job). The extension is the detected format's primary
// extension, never anything user-supplied.
func (s *Service) InputPath(inDir string, itemID uuid.UUID, sourceFormat string) string {
	return filepath.Join(inDir, itemID.String()+"."+primaryExt(sourceFormat))
}

// OutputPath is where an item's converted result is written.
func (s *Service) OutputPath(id, itemID uuid.UUID, target string) string {
	return filepath.Join(s.batchDir(id), "out", itemID.String()+"."+primaryExt(target))
}

// ---- batch creation & enqueue ---------------------------------------------

// CreateBatch persists a batch (already-drafted items) and, when any item is
// pending, enqueues the single processing job. All-failed batches finish
// immediately as failed.
func (s *Service) CreateBatch(ctx context.Context, c *store.Conversion, opts Options) error {
	pending := 0
	for _, it := range c.Items {
		if it.Status == "pending" {
			pending++
		}
	}
	if pending == 0 {
		c.Status = "failed"
	} else {
		c.Status = "queued"
	}
	c.ExpiresAt = time.Now().Add(Retention)
	if err := s.store.CreateConversion(ctx, c); err != nil {
		return err
	}
	if pending == 0 {
		if err := s.store.SetConversionStatus(ctx, c.ID, "failed", true); err != nil {
			s.logger.Warn("finalize all-failed batch", "batch", c.ID, "error", err)
		}
		c.Status = "failed"
		return nil
	}
	payload := batchPayload{BatchID: c.ID, Options: opts}
	if err := s.enqueue(ctx, JobKindBatch, payload); err != nil {
		return err
	}
	return nil
}

type batchPayload struct {
	BatchID uuid.UUID `json:"batchId"`
	Options Options   `json:"options"`
}

// ---- job handlers ----------------------------------------------------------

// RunBatch is the "convert.batch" handler. It processes every pending item with
// a small pool (≤2) — the package-level process semaphore bounds real external
// processes across all batches — then finalizes the batch as done/partial/failed.
func (s *Service) RunBatch(ctx context.Context, payload json.RawMessage) error {
	var p batchPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return err
	}
	conv, err := s.store.GetConversionByID(ctx, p.BatchID)
	if err != nil {
		// The batch may have been deleted or reaped; nothing to do.
		s.logger.Warn("batch not found", "batch", p.BatchID, "error", err)
		return nil
	}
	if err := s.store.SetConversionStatus(ctx, conv.ID, "running", false); err != nil {
		return err
	}
	s.publishBatch(conv.UserID, conv.ID, "running")

	var pending []store.ConversionItem
	for _, it := range conv.Items {
		if it.Status == "pending" {
			pending = append(pending, it)
		}
	}

	poolSize := min(2, len(pending))
	if poolSize > 0 {
		sem := make(chan struct{}, poolSize)
		var wg sync.WaitGroup
		for _, it := range pending {
			wg.Add(1)
			sem <- struct{}{}
			go func(item store.ConversionItem) {
				defer wg.Done()
				defer func() { <-sem }()
				s.processItem(ctx, conv, item, p.Options)
			}(it)
		}
		wg.Wait()
	}

	status := s.finalStatus(ctx, conv.ID)
	if err := s.store.SetConversionStatus(ctx, conv.ID, status, true); err != nil {
		return err
	}
	s.publishBatch(conv.UserID, conv.ID, status)
	return nil
}

func (s *Service) processItem(ctx context.Context, conv *store.Conversion, item store.ConversionItem, opts Options) {
	fail := func(msg string, dur int64) {
		if err := s.store.FailItem(ctx, item.ID, oneLine(msg), dur); err != nil {
			s.logger.Warn("mark item failed", "item", item.ID, "error", err)
		}
		s.publishBatch(conv.UserID, conv.ID, "running")
	}

	e, ok := EdgeFor(item.SourceFormat, conv.TargetFormat)
	if !ok {
		fail("this conversion is not supported", 0)
		return
	}
	if avail, reason := s.Caps().Available(e); !avail {
		fail(reason, 0)
		return
	}

	if err := s.store.SetItemConverting(ctx, item.ID); err != nil {
		s.logger.Warn("mark item converting", "item", item.ID, "error", err)
	}
	s.publishBatch(conv.UserID, conv.ID, "running")

	// The tools run with cwd == the batch dir and address files by relative
	// paths; the service's own I/O uses the absolute equivalents.
	workDir := s.batchDir(conv.ID)
	inRel := filepath.Join("in", item.ID.String()+"."+primaryExt(item.SourceFormat))
	outRel := filepath.Join("out", item.ID.String()+"."+primaryExt(conv.TargetFormat))
	outAbs := filepath.Join(workDir, outRel)
	from, _ := FormatByID(item.SourceFormat)

	start := time.Now()
	cctx, cancel := context.WithTimeout(ctx, timeoutFor(from.Category))
	var runErr error
	if e.Kind == KindTable {
		runErr = s.runTableBounded(cctx, filepath.Join(workDir, inRel), outAbs, item.SourceFormat, conv.TargetFormat)
	} else {
		runErr = s.runner.Convert(cctx, e, workDir, inRel, outRel, "tmp", item.ID.String(), opts)
	}
	cancel()
	dur := time.Since(start).Milliseconds()

	if runErr != nil {
		_ = os.Remove(outAbs)
		fail(runErr.Error(), dur)
		return
	}
	fi, statErr := os.Stat(outAbs)
	if statErr != nil || fi.Size() == 0 {
		fail("the conversion produced no output", dur)
		return
	}
	name := outputName(item.SourceName, conv.TargetFormat)
	if err := s.store.CompleteItem(ctx, item.ID, name, fi.Size(), dur); err != nil {
		s.logger.Warn("complete item", "item", item.ID, "error", err)
	}
	s.publishBatch(conv.UserID, conv.ID, "running")
}

// finalStatus derives the batch verdict from its items.
func (s *Service) finalStatus(ctx context.Context, id uuid.UUID) string {
	conv, err := s.store.GetConversionByID(ctx, id)
	if err != nil {
		return "failed"
	}
	var done, failed int
	for _, it := range conv.Items {
		switch it.Status {
		case "done":
			done++
		case "failed":
			failed++
		}
	}
	switch {
	case done > 0 && failed == 0:
		return "done"
	case done > 0 && failed > 0:
		return "partial"
	default:
		return "failed"
	}
}

// RunCleanup is the "convert.cleanup" handler: it deletes expired batch rows
// and removes their working directories.
func (s *Service) RunCleanup(ctx context.Context, _ json.RawMessage) error {
	ids, err := s.store.PurgeExpiredConversions(ctx)
	if err != nil {
		return err
	}
	for _, id := range ids {
		s.RemoveDir(id)
	}
	if len(ids) > 0 {
		s.logger.Info("reaped expired conversions", "count", len(ids))
	}
	return nil
}

// SweepOnStartup marks batches interrupted by a restart as failed and removes
// any orphaned working directories with no surviving row.
func (s *Service) SweepOnStartup(ctx context.Context) {
	if n, err := s.store.SweepRunningConversions(ctx); err != nil {
		s.logger.Warn("sweep running conversions", "error", err)
	} else if n > 0 {
		s.logger.Warn("marked interrupted conversions as failed", "count", n)
	}
}

// ---- websocket publishing --------------------------------------------------

func (s *Service) publishBatch(userID, batchID uuid.UUID, status string) {
	if s.publish == nil {
		return
	}
	s.publish(userID, "convert", map[string]any{"event": "batch", "batchId": batchID.String(), "status": status})
}

// ---- helpers ---------------------------------------------------------------

func primaryExt(id string) string {
	if f, ok := FormatByID(id); ok {
		return f.PrimaryExt()
	}
	return "bin"
}

func timeoutFor(cat Category) time.Duration {
	switch cat {
	case CategoryVideo, CategoryAnimation:
		return 15 * time.Minute
	case CategoryTable:
		return 1 * time.Minute
	}
	// image, raw, document, audio
	return 5 * time.Minute
}

// outputName derives the user-facing download name: the source's base name with
// the target extension. Sanitized so it is safe in a Content-Disposition header
// and as a zip entry.
func outputName(sourceName, target string) string {
	ext := filepath.Ext(sourceName)
	base := strings.TrimSuffix(sourceName, ext)
	base = sanitizeName(base)
	if base == "" {
		base = "converted"
	}
	return base + "." + primaryExt(target)
}

func sanitizeName(s string) string {
	s = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', '\n', '\r', '"', 0:
			return '_'
		}
		if r < 0x20 {
			return '_'
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}

// maxTableBytes bounds table sources far below the general 1 GiB file cap:
// table conversions parse fully in-process, so a huge JSON/CSV would otherwise
// balloon in RAM outside every sandbox limit. 100 MiB is beyond any real
// spreadsheet.
const maxTableBytes = 100 << 20

// runTableBounded runs a native table conversion under the same discipline as
// external tools: it counts against the process semaphore, refuses oversized
// sources, and gives up waiting at the context deadline. The parse itself is
// uninterruptible Go, but with the size cap its worst case is bounded; on
// timeout the item fails even if the goroutine finishes later (output is
// removed by the caller's error path).
func (s *Service) runTableBounded(ctx context.Context, in, out, fromID, toID string) error {
	if fi, err := os.Stat(in); err == nil && fi.Size() > maxTableBytes {
		return fmt.Errorf("table files are capped at %d MiB for conversion", maxTableBytes>>20)
	}
	select {
	case procSem <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	done := make(chan error, 1)
	go func() {
		defer func() { <-procSem }()
		done <- ConvertTable(in, out, fromID, toID)
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return fmt.Errorf("table conversion timed out")
	}
}
