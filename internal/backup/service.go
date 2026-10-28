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
}

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
	return &Service{
		store:       st,
		vault:       v,
		databaseURL: databaseURL,
		dataDir:     dataDir,
		logger:      logger.With("component", "backup"),
	}
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

// RunAppBackup dumps the database, wraps dump+manifest in a tar archive
// (optionally age-encrypted), uploads it, records the run, and applies
// retention.
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

	// Settle the backup key before dumping. On the first encrypted backup it
	// is created here, and a dump taken before that would not contain it:
	// restoring that archive would then delete the very key it needs.
	var identity *age.X25519Identity
	if row.Encrypt {
		if identity, err = s.encryptionIdentity(ctx); err != nil {
			return "", 0, err
		}
	}

	// pg_dump to a temp file (custom format: compressed, pg_restore-able).
	tmpDir, err := os.MkdirTemp("", "homehub-backup-*")
	if err != nil {
		return "", 0, err
	}
	defer os.RemoveAll(tmpDir)
	dumpPath := filepath.Join(tmpDir, "database.pgdump")

	dumpCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(dumpCtx, "pg_dump", "--format=custom", "--file="+dumpPath, "--dbname="+s.databaseURL)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", 0, fmt.Errorf("pg_dump failed: %s", strings.TrimSpace(string(out)))
	}

	manifest := Manifest{
		Version:    version.Version,
		CreatedAt:  time.Now(),
		Kind:       "app-backup",
		DumpFormat: "pg_dump-custom",
		Encrypted:  row.Encrypt,
	}
	manifestRaw, _ := json.MarshalIndent(manifest, "", "  ")

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

	tarWriter := tar.NewWriter(sink)
	writeEntry := func(name string, size int64, content io.Reader) error {
		if err := tarWriter.WriteHeader(&tar.Header{
			Name: name, Size: size, Mode: 0o640, ModTime: manifest.CreatedAt,
		}); err != nil {
			return err
		}
		_, err := io.Copy(tarWriter, content)
		return err
	}
	if err := writeEntry("manifest.json", int64(len(manifestRaw)), strings.NewReader(string(manifestRaw))); err != nil {
		sink.Close()
		return "", 0, err
	}
	dumpInfo, err := os.Stat(dumpPath)
	if err != nil {
		sink.Close()
		return "", 0, err
	}
	dumpFile, err := os.Open(dumpPath)
	if err != nil {
		sink.Close()
		return "", 0, err
	}
	if err := writeEntry("database.pgdump", dumpInfo.Size(), dumpFile); err != nil {
		dumpFile.Close()
		sink.Close()
		return "", 0, err
	}
	dumpFile.Close()
	if err := tarWriter.Close(); err != nil {
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
	return s.readManifest(ctx, reader, strings.HasSuffix(path, ".age"))
}

func (s *Service) readManifest(ctx context.Context, reader io.Reader, encrypted bool) (*Manifest, error) {
	if encrypted {
		identity, err := s.loadIdentity(ctx, false)
		if err != nil {
			return nil, err
		}
		decrypted, err := age.Decrypt(reader, identity)
		if err != nil {
			return nil, fmt.Errorf("cannot decrypt archive — was it made with this installation's backup key? (%w)", err)
		}
		reader = decrypted
	}
	tarReader := tar.NewReader(reader)
	for {
		header, err := tarReader.Next()
		if err != nil {
			return nil, fmt.Errorf("archive unreadable: %w", err)
		}
		if header.Name == "manifest.json" {
			var manifest Manifest
			if err := json.NewDecoder(io.LimitReader(tarReader, 1<<20)).Decode(&manifest); err != nil {
				return nil, err
			}
			return &manifest, nil
		}
	}
}

// Restore downloads an archive and replays the database dump over the live
// database, then requests a process restart so every subsystem reloads
// from the restored state.
func (s *Service) Restore(ctx context.Context, targetID uuid.UUID, path string) error {
	row, err := s.store.GetBackupTarget(ctx, targetID)
	if err != nil {
		return fmt.Errorf("backup target not found")
	}
	runID, err := s.store.StartBackupRun(ctx, row.ID, row.Name, "restore")
	if err != nil {
		return err
	}
	if err := s.restore(ctx, row, path); err != nil {
		_ = s.store.FinishBackupRun(ctx, runID, "failed", path, 0, err.Error())
		return err
	}
	// pg_restore --clean replaced the whole database, taking the run row
	// created above with it — record the outcome into the restored state.
	_ = s.store.RecordBackupRun(ctx, row.ID, row.Name, "restore", "succeeded", path, "")
	s.logger.Warn("database restored — restarting to reload all state", "archive", path)
	if s.RequestRestart != nil {
		// Give the HTTP response time to flush before the process exits.
		go func() {
			time.Sleep(2 * time.Second)
			s.RequestRestart()
		}()
	}
	return nil
}

func (s *Service) restore(ctx context.Context, row *store.BackupTarget, path string) error {
	target, err := s.OpenTarget(ctx, row)
	if err != nil {
		return err
	}
	defer target.Close()

	reader, err := target.Read(ctx, path)
	if err != nil {
		return fmt.Errorf("cannot read archive: %w", err)
	}
	defer reader.Close()

	var stream io.Reader = reader
	if strings.HasSuffix(path, ".age") {
		identity, err := s.loadIdentity(ctx, false)
		if err != nil {
			return err
		}
		decrypted, err := age.Decrypt(stream, identity)
		if err != nil {
			return fmt.Errorf("cannot decrypt archive — was it made with this installation's backup key? (%w)", err)
		}
		stream = decrypted
	}

	tmpDir, err := os.MkdirTemp("", "homehub-restore-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	dumpPath := filepath.Join(tmpDir, "database.pgdump")

	tarReader := tar.NewReader(stream)
	found := false
	for {
		header, terr := tarReader.Next()
		if terr == io.EOF {
			break
		}
		if terr != nil {
			return fmt.Errorf("archive unreadable: %w", terr)
		}
		if header.Name == "database.pgdump" {
			out, oerr := os.Create(dumpPath)
			if oerr != nil {
				return oerr
			}
			if _, cerr := io.Copy(out, tarReader); cerr != nil {
				out.Close()
				return cerr
			}
			out.Close()
			found = true
		}
	}
	if !found {
		return fmt.Errorf("archive does not contain a database dump")
	}

	restoreCtx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(restoreCtx, "pg_restore",
		"--clean", "--if-exists", "--no-owner", "--dbname="+s.databaseURL, dumpPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("pg_restore failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}
