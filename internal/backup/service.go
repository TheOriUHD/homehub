package backup

import (
	"archive/tar"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"filippo.io/age"
	"github.com/google/uuid"

	"homehub/internal/db"
	"homehub/internal/store"
	"homehub/internal/vault"
	"homehub/internal/version"
)

// JobPayload parameterizes the backup.app and backup.restore job kinds and
// backup schedule payloads.
type JobPayload struct {
	TargetID uuid.UUID `json:"targetId"`
	UserID   uuid.UUID `json:"userId,omitempty"`
	Path     string    `json:"path,omitempty"`
}

// Manifest describes one archive, stored alongside the dump inside it.
type Manifest struct {
	Version    string    `json:"version"`
	CreatedAt  time.Time `json:"createdAt"`
	Kind       string    `json:"kind"`
	DumpFormat string    `json:"dumpFormat"`
	Encrypted  bool      `json:"encrypted"`
	// SchemaVersion is the newest migration applied to the dumped database.
	// Archives from before it was recorded leave it zero; those all predate
	// any binary that can read this field, so they are older by definition.
	SchemaVersion int64 `json:"schemaVersion,omitempty"`
	// Workspace is true when the archive carries the uploaded workspace
	// files under workspace/. Older archives hold only the database, and
	// restoring one leaves the files on disk as they are.
	Workspace bool `json:"workspace,omitempty"`
}

const (
	manifestEntry  = "manifest.json"
	dumpEntry      = "database.pgdump"
	workspaceEntry = "workspace/"
	// workspaceDir is where uploaded workspace blobs live under the data dir
	// ({DataDir}/workspace/{fileID}); the files API owns the layout.
	workspaceDir = "workspace"
	// workPrefix names scratch directories under the data dir. They hold
	// archives and staged restores — too big for a tmpfs /tmp once workspace
	// files are included, and on the same filesystem as the workspace so a
	// restored copy can be renamed into place.
	workPrefix = ".backup-work-"
)

// secretStore is the slice of the vault the backup service uses.
type secretStore interface {
	Open(ctx context.Context, scope, name string) ([]byte, error)
	Seal(ctx context.Context, scope, name string, plaintext []byte) error
}

// Service runs application backups, restores, and retention.
type Service struct {
	store       *store.Store
	vault       secretStore
	databaseURL string
	dataDir     string
	logger      *slog.Logger
	// identityMu serializes creation of the backup key, so two first
	// backups running at once cannot each seal a different one.
	identityMu sync.Mutex
	// RequestRestart asks the process supervisor for a restart after a
	// successful restore; wired by the app.
	RequestRestart func()
}

func NewService(st *store.Store, v *vault.Vault, databaseURL, dataDir string, logger *slog.Logger) *Service {
	s := &Service{
		store:       st,
		vault:       v,
		databaseURL: databaseURL,
		dataDir:     dataDir,
		logger:      logger.With("component", "backup"),
	}
	s.sweepWorkDirs()
	return s
}

// sweepWorkDirs removes scratch directories left by a backup or restore that
// was interrupted by a crash or restart.
func (s *Service) sweepWorkDirs() {
	if s.dataDir == "" {
		return
	}
	matches, _ := filepath.Glob(filepath.Join(s.dataDir, workPrefix+"*"))
	for _, dir := range matches {
		if err := os.RemoveAll(dir); err != nil {
			s.logger.Warn("removing leftover backup scratch directory failed", "dir", dir, "error", err)
		}
	}
}

func (s *Service) workDir() (string, error) {
	return os.MkdirTemp(s.dataDir, workPrefix+"*")
}

// OpenTarget resolves a stored target row into a live client.
func (s *Service) OpenTarget(ctx context.Context, row *store.BackupTarget) (Target, error) {
	config := map[string]any{}
	_ = json.Unmarshal(row.Config, &config)
	secrets := map[string]string{}
	for _, name := range TargetTypes[row.Type] {
		raw, err := s.vault.Open(ctx, "target:"+row.ID.String(), name)
		switch {
		case err == nil:
			secrets[name] = string(raw)
		case errors.Is(err, vault.ErrNotFound):
			// Optional secrets (e.g. a guest share's password) are simply unset.
		default:
			return nil, fmt.Errorf("cannot read the stored %s for this target: %w", name, err)
		}
	}
	return Open(ctx, row.Type, config, secrets)
}

var errNoBackupKey = errors.New("this installation has no backup encryption key yet, so it cannot have made this encrypted archive")

// loadIdentity returns the age key for encrypted archives. It is a dedicated
// secret so app backups can be decrypted with a key the admin can export,
// independent of the vault master key (which must never live inside a
// backup).
//
// A new key is only made when none is stored. Any other failure to read it —
// the database is down, the master key is wrong, the row is damaged — is
// returned as is: replacing the key then would leave every archive encrypted
// with the old one undecryptable, with no sign of it until a restore fails.
func (s *Service) loadIdentity(ctx context.Context, create bool) (*age.X25519Identity, error) {
	s.identityMu.Lock()
	defer s.identityMu.Unlock()
	raw, err := s.vault.Open(ctx, "backup", "age-identity")
	switch {
	case err == nil:
		identity, perr := age.ParseX25519Identity(strings.TrimSpace(string(raw)))
		if perr != nil {
			return nil, fmt.Errorf("the stored backup encryption key is damaged: %w", perr)
		}
		return identity, nil
	case !errors.Is(err, vault.ErrNotFound):
		return nil, fmt.Errorf("cannot read the backup encryption key: %w", err)
	case !create:
		return nil, errNoBackupKey
	}
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		return nil, err
	}
	if err := s.vault.Seal(ctx, "backup", "age-identity", []byte(identity.String())); err != nil {
		return nil, err
	}
	return identity, nil
}

// encryptionIdentity returns the backup key, creating it on first use.
func (s *Service) encryptionIdentity(ctx context.Context) (*age.X25519Identity, error) {
	return s.loadIdentity(ctx, true)
}

// ExportEncryptionKey returns the age identity string for admin download.
func (s *Service) ExportEncryptionKey(ctx context.Context) (string, error) {
	identity, err := s.encryptionIdentity(ctx)
	if err != nil {
		return "", err
	}
	return identity.String(), nil
}

// RunAppBackup dumps the database, wraps dump+manifest+workspace files in a
// tar archive (optionally age-encrypted), uploads it, records the run, and
// applies retention.
func (s *Service) RunAppBackup(ctx context.Context, targetID uuid.UUID) (string, error) {
	row, err := s.store.GetBackupTarget(ctx, targetID)
	if err != nil {
		return "", fmt.Errorf("backup target not found")
	}
	runID, err := s.store.StartBackupRun(ctx, row.ID, row.Name, "app-backup")
	if err != nil {
		return "", err
	}
	filePath, size, err := s.runAppBackup(ctx, row)
	if err != nil {
		_ = s.store.FinishBackupRun(ctx, runID, "failed", filePath, 0, err.Error())
		_ = s.store.SetBackupTargetStatus(ctx, row.ID, "error", err.Error())
		return "", err
	}
	_ = s.store.FinishBackupRun(ctx, runID, "succeeded", filePath, size, "")
	_ = s.store.SetBackupTargetStatus(ctx, row.ID, "ok", "")

	if pruned, perr := s.prune(ctx, row); perr != nil {
		s.logger.Warn("retention pruning failed", "target", row.Name, "error", perr)
	} else if pruned > 0 {
		s.logger.Info("pruned old backups", "target", row.Name, "count", pruned)
	}
	return filePath, nil
}

func (s *Service) runAppBackup(ctx context.Context, row *store.BackupTarget) (string, int64, error) {
	target, err := s.OpenTarget(ctx, row)
	if err != nil {
		return "", 0, err
	}
	defer target.Close()

	schemaVersion, err := db.AppliedVersion(ctx, s.store.Pool())
	if err != nil {
		return "", 0, err
	}
	// Settle the backup key before dumping. On the first encrypted backup it
	// is created here, and a dump taken before that would not contain it:
	// restoring that archive would then delete the very key it needs.
	var identity *age.X25519Identity
	if row.Encrypt {
		if identity, err = s.encryptionIdentity(ctx); err != nil {
			return "", 0, err
		}
	}

	// pg_dump to a scratch file (custom format: compressed, pg_restore-able).
	tmpDir, err := s.workDir()
	if err != nil {
		return "", 0, err
	}
	defer os.RemoveAll(tmpDir)
	dumpPath := filepath.Join(tmpDir, dumpEntry)

	dumpCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(dumpCtx, "pg_dump", "--format=custom", "--file="+dumpPath, "--dbname="+s.databaseURL)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", 0, fmt.Errorf("pg_dump failed: %s", strings.TrimSpace(string(out)))
	}

	manifest := Manifest{
		Version:       version.Version,
		CreatedAt:     time.Now(),
		Kind:          "app-backup",
		DumpFormat:    "pg_dump-custom",
		Encrypted:     row.Encrypt,
		SchemaVersion: schemaVersion,
		Workspace:     true,
	}

	archivePath := filepath.Join(tmpDir, "archive")
	archiveFile, err := os.Create(archivePath)
	if err != nil {
		return "", 0, err
	}

	var sink io.WriteCloser = archiveFile
	if identity != nil {
		encWriter, eerr := age.Encrypt(archiveFile, identity.Recipient())
		if eerr != nil {
			archiveFile.Close()
			return "", 0, eerr
		}
		sink = &chainedCloser{Writer: encWriter, closers: []io.Closer{encWriter, archiveFile}}
	}
	if err := writeArchive(sink, manifest, dumpPath, filepath.Join(s.dataDir, workspaceDir)); err != nil {
		sink.Close()
		return "", 0, err
	}
	if err := sink.Close(); err != nil {
		return "", 0, err
	}

	suffix := ".tar"
	if row.Encrypt {
		suffix = ".tar.age"
	}
	remotePath := "homehub/app-backup-" + manifest.CreatedAt.Format("20060102-150405") + suffix

	info, err := os.Stat(archivePath)
	if err != nil {
		return "", 0, err
	}
	upload, err := os.Open(archivePath)
	if err != nil {
		return "", 0, err
	}
	defer upload.Close()
	if err := target.Write(ctx, remotePath, upload, info.Size()); err != nil {
		return remotePath, 0, fmt.Errorf("upload failed: %w", err)
	}
	return remotePath, info.Size(), nil
}

// writeArchive lays out an archive: the manifest first (so it can be checked
// before anything large is read back), then the database dump, then every
// uploaded workspace blob as workspace/<file id>.
func writeArchive(w io.Writer, manifest Manifest, dumpPath, workspace string) error {
	manifestRaw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	tw := tar.NewWriter(w)
	writeEntry := func(name string, size int64, content io.Reader) error {
		if err := tw.WriteHeader(&tar.Header{
			Name: name, Size: size, Mode: 0o640, ModTime: manifest.CreatedAt,
		}); err != nil {
			return err
		}
		// A file that grows while it is copied must not overrun its header.
		_, err := io.CopyN(tw, content, size)
		return err
	}
	if err := writeEntry(manifestEntry, int64(len(manifestRaw)), strings.NewReader(string(manifestRaw))); err != nil {
		return err
	}
	if err := writeFileEntry(dumpEntry, dumpPath, writeEntry); err != nil {
		return err
	}
	if manifest.Workspace {
		entries, err := os.ReadDir(workspace)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("cannot read workspace files: %w", err)
		}
		for _, entry := range entries {
			// Only blobs the files API wrote: a regular file named by its id.
			if !entry.Type().IsRegular() || !isWorkspaceBlobName(entry.Name()) {
				continue
			}
			err := writeFileEntry(workspaceEntry+entry.Name(), filepath.Join(workspace, entry.Name()), writeEntry)
			if errors.Is(err, os.ErrNotExist) {
				// Deleted by its owner since the listing; nothing to keep.
				continue
			}
			if err != nil {
				return fmt.Errorf("workspace file %s: %w", entry.Name(), err)
			}
		}
	}
	return tw.Close()
}

func writeFileEntry(name, path string, writeEntry func(string, int64, io.Reader) error) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	return writeEntry(name, info.Size(), file)
}

// isWorkspaceBlobName reports whether name is a workspace blob's file name:
// the canonical form of its file id, nothing else.
func isWorkspaceBlobName(name string) bool {
	id, err := uuid.Parse(name)
	return err == nil && id.String() == name
}

type chainedCloser struct {
	io.Writer
	closers []io.Closer
}

func (c *chainedCloser) Close() error {
	var first error
	for _, closer := range c.closers {
		if err := closer.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// prune removes archives older than the target's retention window.
func (s *Service) prune(ctx context.Context, row *store.BackupTarget) (int, error) {
	target, err := s.OpenTarget(ctx, row)
	if err != nil {
		return 0, err
	}
	defer target.Close()
	entries, err := target.List(ctx, "homehub/")
	if err != nil {
		return 0, err
	}
	cutoff := time.Now().AddDate(0, 0, -row.RetentionDays)
	pruned := 0
	for _, entry := range entries {
		if entry.ModTime.Before(cutoff) {
			if err := target.Delete(ctx, entry.Path); err == nil {
				pruned++
			}
		}
	}
	return pruned, nil
}

// InspectArchive downloads an archive's manifest without extracting data.
func (s *Service) InspectArchive(ctx context.Context, row *store.BackupTarget, path string) (*Manifest, error) {
	target, err := s.OpenTarget(ctx, row)
	if err != nil {
		return nil, err
	}
	defer target.Close()
	reader, err := target.Read(ctx, path)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	stream, err := s.decryptStream(ctx, reader, strings.HasSuffix(path, ".age"))
	if err != nil {
		return nil, err
	}
	tarReader := tar.NewReader(stream)
	for {
		header, err := tarReader.Next()
		if err != nil {
			return nil, fmt.Errorf("archive unreadable: %w", err)
		}
		if header.Name == manifestEntry {
			return decodeManifest(tarReader)
		}
	}
}

func decodeManifest(r io.Reader) (*Manifest, error) {
	var manifest Manifest
	if err := json.NewDecoder(io.LimitReader(r, 1<<20)).Decode(&manifest); err != nil {
		return nil, fmt.Errorf("archive manifest unreadable: %w", err)
	}
	return &manifest, nil
}

func (s *Service) decryptStream(ctx context.Context, r io.Reader, encrypted bool) (io.Reader, error) {
	if !encrypted {
		return r, nil
	}
	identity, err := s.loadIdentity(ctx, false)
	if err != nil {
		return nil, err
	}
	decrypted, err := age.Decrypt(r, identity)
	if err != nil {
		return nil, fmt.Errorf("cannot decrypt archive — was it made with this installation's backup key? (%w)", err)
	}
	return decrypted, nil
}

// Restore downloads an archive and replays it over the live state, then
// requests a process restart so every subsystem reloads from the restored
// state. The database is replaced in one transaction: a restore that fails
// at any point leaves it exactly as it was.
func (s *Service) Restore(ctx context.Context, targetID uuid.UUID, path string) error {
	row, err := s.store.GetBackupTarget(ctx, targetID)
	if err != nil {
		return fmt.Errorf("backup target not found")
	}
	runID, err := s.store.StartBackupRun(ctx, row.ID, row.Name, "restore")
	if err != nil {
		return err
	}
	dbRestored, err := s.restore(ctx, row, path)
	if !dbRestored {
		// Nothing changed, so the run row created above is still there.
		_ = s.store.FinishBackupRun(ctx, runID, "failed", path, 0, err.Error())
		return err
	}
	// The restore replaced the whole database, taking the run row created
	// above with it — record the outcome into the restored state.
	status, message := "succeeded", ""
	if err != nil {
		status, message = "failed", err.Error()
	}
	_ = s.store.RecordBackupRun(ctx, row.ID, row.Name, "restore", status, path, message)
	s.logger.Warn("database restored — restarting to reload all state", "archive", path, "error", err)
	if s.RequestRestart != nil {
		// Give the HTTP response time to flush before the process exits.
		go func() {
			time.Sleep(2 * time.Second)
			s.RequestRestart()
		}()
	}
	return err
}

// restore reports whether the database was replaced; when it was, a non-nil
// error means the workspace files could not be put in place afterwards.
func (s *Service) restore(ctx context.Context, row *store.BackupTarget, path string) (bool, error) {
	supported, err := db.SupportedVersion()
	if err != nil {
		return false, err
	}
	target, err := s.OpenTarget(ctx, row)
	if err != nil {
		return false, err
	}
	defer target.Close()

	reader, err := target.Read(ctx, path)
	if err != nil {
		return false, fmt.Errorf("cannot read archive: %w", err)
	}
	defer reader.Close()
	stream, err := s.decryptStream(ctx, reader, strings.HasSuffix(path, ".age"))
	if err != nil {
		return false, err
	}

	stageDir, err := s.workDir()
	if err != nil {
		return false, err
	}
	defer os.RemoveAll(stageDir)
	staged, err := stageArchive(stream, stageDir, supported)
	if err != nil {
		return false, err
	}

	restoreCtx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	if err := restoreDatabase(restoreCtx, s.databaseURL, staged.dumpPath); err != nil {
		return false, err
	}
	if staged.workspace != "" {
		if err := swapWorkspace(filepath.Join(s.dataDir, workspaceDir), staged.workspace, stageDir); err != nil {
			s.logger.Error("database restored but workspace files could not be put in place", "error", err)
			return true, fmt.Errorf("the database was restored, but the workspace files could not be put in place: %w", err)
		}
	}
	return true, nil
}

type stagedArchive struct {
	manifest *Manifest
	dumpPath string
	// workspace is the directory holding the archive's workspace files, or
	// "" for an archive that carries none (restoring it leaves files alone).
	workspace string
}

// stageArchive unpacks an archive into stageDir, refusing it before anything
// large is written when it was made by a newer schema than this binary has.
// Only the entries a backup writes are accepted; nothing in the archive
// chooses where on disk it lands.
func stageArchive(r io.Reader, stageDir string, supportedSchema int64) (*stagedArchive, error) {
	out := &stagedArchive{}
	tr := tar.NewReader(r)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("archive unreadable: %w", err)
		}
		switch {
		case header.Name == manifestEntry:
			manifest, err := decodeManifest(tr)
			if err != nil {
				return nil, err
			}
			if manifest.SchemaVersion > supportedSchema {
				return nil, fmt.Errorf("this archive was made by a newer HomeHub (version %s, database schema %d); "+
					"this one only understands schema %d — update HomeHub first, then restore",
					manifest.Version, manifest.SchemaVersion, supportedSchema)
			}
			out.manifest = manifest
			if manifest.Workspace {
				out.workspace = filepath.Join(stageDir, workspaceDir)
				if err := os.Mkdir(out.workspace, 0o700); err != nil {
					return nil, err
				}
			}
		case out.manifest == nil:
			// Every archive starts with its manifest; without it the
			// schema check above could not have run.
			return nil, errors.New("archive has no manifest before its data — not a HomeHub backup")
		case header.Name == dumpEntry:
			out.dumpPath = filepath.Join(stageDir, dumpEntry)
			if err := extractTo(out.dumpPath, tr, 0o600); err != nil {
				return nil, err
			}
		case strings.HasPrefix(header.Name, workspaceEntry):
			name := strings.TrimPrefix(header.Name, workspaceEntry)
			if out.workspace == "" || header.Typeflag != tar.TypeReg || !isWorkspaceBlobName(name) {
				return nil, fmt.Errorf("archive holds an unexpected entry %q", header.Name)
			}
			if err := extractTo(filepath.Join(out.workspace, name), tr, 0o600); err != nil {
				return nil, err
			}
		}
	}
	if out.manifest == nil {
		return nil, errors.New("archive has no manifest — not a HomeHub backup")
	}
	if out.dumpPath == "" {
		return nil, errors.New("archive does not contain a database dump")
	}
	return out, nil
}

func extractTo(path string, r io.Reader, mode os.FileMode) error {
	out, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, r); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// clearSchemaSQL drops every table, view and sequence in the public schema
// ahead of the replay. pg_restore --clean only drops what the archive itself
// contains, so restoring an older archive would leave newer tables behind
// with the schema version rolled back, and the migrations on the next start
// would fail creating tables that already exist. The same goes for the
// schema's functions (the audit log's append-only trigger function), which
// are dropped after the tables that use them; HomeHub defines no types or
// extensions of its own.
const clearSchemaSQL = `
set local client_min_messages = warning;
do $$
declare r record;
begin
	for r in
		select c.relname, c.relkind from pg_class c
		join pg_namespace n on n.oid = c.relnamespace
		where n.nspname = 'public' and c.relkind in ('r', 'p', 'v', 'm', 'S')
		-- Tables first: identity sequences cannot be dropped on their own and
		-- go with their table; "if exists" skips them afterwards.
		order by c.relkind = 'S'
	loop
		execute format('drop %s if exists public.%I cascade',
			case r.relkind when 'v' then 'view' when 'm' then 'materialized view'
			               when 'S' then 'sequence' else 'table' end,
			r.relname);
	end loop;
	for r in
		select p.oid::regprocedure as sig from pg_proc p
		join pg_namespace n on n.oid = p.pronamespace
		where n.nspname = 'public' and p.prokind = 'f'
	loop
		execute format('drop function if exists %s cascade', r.sig);
	end loop;
end $$;`

// settleJobsSQL runs last, against the restored rows. The dump may have caught
// a restore that was queued at the time; replaying it after the restart would
// restore again, over whatever happened since. Schema-qualified because the
// replayed dump empties search_path.
const settleJobsSQL = `
update public.jobs set status = 'failed', finished_at = now(),
	last_error = 'not resumed: the database was restored from a backup'
where kind = 'backup.restore' and status in ('pending', 'running');`

// restoreDatabase replaces the live database with the dump in a single
// transaction: pg_restore renders the dump as SQL, and psql replays it after
// clearing the schema, with --single-transaction and ON_ERROR_STOP so the
// first error rolls everything back. A failed restore leaves the database
// exactly as it was, rather than half-dropped with the app unable to start.
//
// The script is rendered to a file and checked before replaying it, never
// piped: psql commits once its input ends, so a renderer that died halfway
// would otherwise commit a truncated restore.
func restoreDatabase(ctx context.Context, databaseURL, dumpPath string) error {
	scriptPath := dumpPath + ".sql"
	defer os.Remove(scriptPath)
	render := exec.CommandContext(ctx, "pg_restore",
		"--no-owner", "--no-privileges", "--no-comments", "--exit-on-error",
		"--file="+scriptPath, dumpPath)
	if out, err := render.CombinedOutput(); err != nil {
		return fmt.Errorf("the archive's database dump is unreadable, nothing was changed: %s", strings.TrimSpace(string(out)))
	}
	replay := exec.CommandContext(ctx, "psql",
		"--no-psqlrc", "--quiet", "--single-transaction", "--set=ON_ERROR_STOP=1",
		// Only errors matter; the replay's own query output is noise.
		"--output="+os.DevNull,
		"--dbname="+databaseURL,
		"--command="+clearSchemaSQL, "--file="+scriptPath, "--command="+settleJobsSQL)
	if out, err := replay.CombinedOutput(); err != nil {
		return fmt.Errorf("restore failed, the database was left unchanged: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// swapWorkspace puts a restored workspace directory in place of the live one.
// Both sit on the same filesystem, so each step is a rename; the previous
// files move into the scratch directory and are removed with it.
func swapWorkspace(live, restored, scratch string) error {
	old := filepath.Join(scratch, "workspace.previous")
	hadLive := true
	if err := os.Rename(live, old); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		hadLive = false
	}
	if err := os.Rename(restored, live); err != nil {
		if hadLive {
			_ = os.Rename(old, live)
		}
		return err
	}
	return nil
}
