package backup

import (
	"context"
	"io"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"homehub/internal/db"
	"homehub/internal/store"
	"homehub/internal/vault"
)

// The restore replays over a whole database, dropping everything in it, so it
// only runs against a database set aside for it: HOMEHUB_RESTORE_TEST_DATABASE_URL,
// never the shared HOMEHUB_DATABASE_URL the other database tests use. A
// database named "homehub" is refused outright.
func restoreTestDB(t *testing.T) (string, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("HOMEHUB_RESTORE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("HOMEHUB_RESTORE_TEST_DATABASE_URL not set (a scratch database this test may wipe)")
	}
	if u, err := url.Parse(dsn); err != nil || strings.Trim(u.Path, "/") == "homehub" || strings.Trim(u.Path, "/") == "" {
		t.Fatalf("refusing to wipe %q: name a scratch database", dsn)
	}
	ctx := context.Background()
	// Start from an empty schema so an earlier failed run cannot skew this one.
	if out, err := exec.Command("psql", "--no-psqlrc", "--quiet", "--set=ON_ERROR_STOP=1",
		"--dbname="+dsn, "--command="+clearSchemaSQL).CombinedOutput(); err != nil {
		t.Fatalf("clear scratch database: %s", out)
	}
	if err := db.Migrate(ctx, dsn, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return dsn, pool
}

func exec1(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

func count(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return n
}

func dumpTo(t *testing.T, dsn, path string) {
	t.Helper()
	if out, err := exec.Command("pg_dump", "--format=custom", "--file="+path, "--dbname="+dsn).CombinedOutput(); err != nil {
		t.Fatalf("pg_dump: %s", out)
	}
}

func TestRestoreIsAllOrNothing(t *testing.T) {
	dsn, pool := restoreTestDB(t)
	ctx := context.Background()
	dir := t.TempDir()

	exec1(t, pool, `delete from ssh_known_hosts`)
	exec1(t, pool, `insert into ssh_known_hosts (host, port, fingerprint) values ('backed-up', 22, 'SHA256:a')`)
	exec1(t, pool, `insert into jobs (kind, status) values ('backup.restore', 'pending')`)
	good := filepath.Join(dir, "good.pgdump")
	dumpTo(t, dsn, good)

	// A dump whose replay fails at the very end (the jobs table it settles
	// is missing), after every table was already dropped and recreated.
	exec1(t, pool, `drop table jobs`)
	broken := filepath.Join(dir, "broken.pgdump")
	dumpTo(t, dsn, broken)
	if err := restoreDatabase(ctx, dsn, good); err != nil {
		t.Fatalf("restore good dump: %v", err)
	}

	// State written after the backup, including a table a newer migration
	// would have made.
	exec1(t, pool, `insert into ssh_known_hosts (host, port, fingerprint) values ('after', 22, 'SHA256:b')`)
	exec1(t, pool, `create table zz_newer_feature (id int)`)

	err := restoreDatabase(ctx, dsn, broken)
	if err == nil || !strings.Contains(err.Error(), "jobs") {
		t.Fatalf("restore of a dump failing at its last step: %v", err)
	}
	if count(t, pool, `select count(*) from ssh_known_hosts where host = 'after'`) != 1 ||
		count(t, pool, `select count(*) from pg_tables where tablename in ('zz_newer_feature', 'jobs')`) != 2 {
		t.Fatal("a failed restore changed the database")
	}

	if err := os.WriteFile(filepath.Join(dir, "garbage.pgdump"), []byte("not a dump"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := restoreDatabase(ctx, dsn, filepath.Join(dir, "garbage.pgdump")); err == nil {
		t.Fatal("garbage dump restored")
	}

	if err := restoreDatabase(ctx, dsn, good); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if count(t, pool, `select count(*) from ssh_known_hosts where host = 'after'`) != 0 ||
		count(t, pool, `select count(*) from ssh_known_hosts where host = 'backed-up'`) != 1 {
		t.Fatal("restored rows are not the backed-up ones")
	}
	if count(t, pool, `select count(*) from pg_tables where tablename = 'zz_newer_feature'`) != 0 {
		t.Fatal("a table missing from the archive survived the restore")
	}
	if count(t, pool, `select count(*) from jobs where kind = 'backup.restore' and status = 'pending'`) != 0 {
		t.Fatal("a restore job captured in the dump would run again after the restart")
	}

	// The restored database still migrates cleanly, as the next start will.
	if err := db.Migrate(ctx, dsn, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("migrate after restore: %v", err)
	}
	applied, _ := db.AppliedVersion(ctx, pool)
	supported, _ := db.SupportedVersion()
	if applied != supported {
		t.Fatalf("applied %d, supported %d", applied, supported)
	}
}

// The whole loop through the service: an encrypted backup to a local target
// carries the database and the workspace files, and restoring it brings both
// back as they were.
func TestBackupAndRestoreRoundTripWithWorkspaceFiles(t *testing.T) {
	dsn, pool := restoreTestDB(t)
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	dataDir := t.TempDir()
	st := store.New(pool)
	v, err := vault.Open(pool, dataDir, "", logger)
	if err != nil {
		t.Fatalf("vault: %v", err)
	}
	svc := NewService(st, v, dsn, dataDir, logger)

	targetDir := t.TempDir()
	row, err := st.CreateBackupTarget(ctx, "local", "scratch", map[string]any{"path": targetDir}, 30, true)
	if err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(dataDir, workspaceDir)
	_ = os.Mkdir(workspace, 0o700)
	kept := uuid.NewString()
	_ = os.WriteFile(filepath.Join(workspace, kept), []byte("kept"), 0o600)
	exec1(t, pool, `insert into ssh_known_hosts (host, port, fingerprint) values ('round-trip', 22, 'SHA256:a')`)

	archive, err := svc.RunAppBackup(ctx, row.ID)
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	manifest, err := svc.InspectArchive(ctx, row, archive)
	if err != nil || !manifest.Workspace || manifest.SchemaVersion == 0 {
		t.Fatalf("manifest %+v, %v", manifest, err)
	}

	later := uuid.NewString()
	_ = os.WriteFile(filepath.Join(workspace, later), []byte("later"), 0o600)
	_ = os.Remove(filepath.Join(workspace, kept))
	exec1(t, pool, `delete from ssh_known_hosts where host = 'round-trip'`)

	if err := svc.Restore(ctx, row.ID, archive); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(workspace, kept)); string(got) != "kept" {
		t.Fatalf("workspace file not restored: %q", got)
	}
	if _, err := os.Stat(filepath.Join(workspace, later)); !os.IsNotExist(err) {
		t.Fatal("a file uploaded after the backup survived the restore")
	}
	if count(t, pool, `select count(*) from ssh_known_hosts where host = 'round-trip'`) != 1 {
		t.Fatal("database not restored")
	}
	if matches, _ := filepath.Glob(filepath.Join(dataDir, workPrefix+"*")); len(matches) != 0 {
		t.Fatalf("scratch directories left behind: %v", matches)
	}

	// The first encrypted backup created the backup key; the restored
	// database must still hold it, or the archive just restored from (and
	// every other) could never be opened again.
	if _, err := svc.InspectArchive(ctx, row, archive); err != nil {
		t.Fatalf("archive unreadable after restoring it: %v", err)
	}

	// The backup key is now sealed in the database, so a data dir that lost
	// its master.key must not get a fresh one.
	if _, err := vault.Open(pool, t.TempDir(), "", logger); err == nil {
		t.Fatal("vault generated a new master key over existing secrets")
	}
}
