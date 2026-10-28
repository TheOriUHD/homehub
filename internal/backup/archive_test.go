package backup

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/google/uuid"
)

func writeTestArchive(t *testing.T, manifest Manifest, workspace string) []byte {
	t.Helper()
	dump := filepath.Join(t.TempDir(), "database.pgdump")
	if err := os.WriteFile(dump, []byte("PGDMP-test"), 0o600); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := writeArchive(&buf, manifest, dump, workspace); err != nil {
		t.Fatalf("writeArchive: %v", err)
	}
	return buf.Bytes()
}

func TestArchiveCarriesWorkspaceFilesAndRestoresThem(t *testing.T) {
	workspace := t.TempDir()
	id := uuid.NewString()
	if err := os.WriteFile(filepath.Join(workspace, id), []byte("photo bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Not blobs: skipped, never archived.
	_ = os.WriteFile(filepath.Join(workspace, "notes.txt"), []byte("x"), 0o600)
	_ = os.Mkdir(filepath.Join(workspace, uuid.NewString()), 0o700)

	archive := writeTestArchive(t, Manifest{Version: "test", SchemaVersion: 39, Workspace: true}, workspace)

	stage := t.TempDir()
	staged, err := stageArchive(bytes.NewReader(archive), stage, 39)
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	if got, _ := os.ReadFile(staged.dumpPath); string(got) != "PGDMP-test" {
		t.Fatalf("dump = %q", got)
	}
	entries, _ := os.ReadDir(staged.workspace)
	if len(entries) != 1 || entries[0].Name() != id {
		t.Fatalf("staged workspace = %v", entries)
	}

	// The staged copy replaces the live directory wholesale.
	live := filepath.Join(t.TempDir(), "workspace")
	_ = os.Mkdir(live, 0o700)
	_ = os.WriteFile(filepath.Join(live, uuid.NewString()), []byte("uploaded after the backup"), 0o600)
	if err := swapWorkspace(live, staged.workspace, stage); err != nil {
		t.Fatalf("swap: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(live, id)); string(got) != "photo bytes" {
		t.Fatalf("restored blob = %q", got)
	}
	if entries, _ := os.ReadDir(live); len(entries) != 1 {
		t.Fatalf("live workspace has %d entries, want exactly the archived one", len(entries))
	}
}

func TestEmptyWorkspaceStillMarksTheArchive(t *testing.T) {
	archive := writeTestArchive(t, Manifest{Workspace: true}, filepath.Join(t.TempDir(), "missing"))
	staged, err := stageArchive(bytes.NewReader(archive), t.TempDir(), 39)
	if err != nil {
		t.Fatal(err)
	}
	// An archive of an empty workspace restores an empty workspace.
	if staged.workspace == "" {
		t.Fatal("workspace-carrying archive staged without a workspace")
	}
}

// Archives from before workspace files and schema versions were recorded must
// still restore, and leave the files on disk alone.
func TestOldArchiveFormatStillRestores(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	manifest, _ := json.Marshal(map[string]any{
		"version": "0.9.0", "createdAt": time.Now(), "kind": "app-backup",
		"dumpFormat": "pg_dump-custom", "encrypted": false,
	})
	for _, e := range []struct {
		name string
		body []byte
	}{{"manifest.json", manifest}, {"database.pgdump", []byte("PGDMP-old")}} {
		_ = tw.WriteHeader(&tar.Header{Name: e.name, Size: int64(len(e.body)), Mode: 0o640})
		_, _ = tw.Write(e.body)
	}
	_ = tw.Close()

	staged, err := stageArchive(&buf, t.TempDir(), 39)
	if err != nil {
		t.Fatalf("old archive refused: %v", err)
	}
	if staged.workspace != "" {
		t.Fatal("old archive would replace the workspace files")
	}
}

func TestNewerSchemaArchiveIsRefusedBeforeRestoring(t *testing.T) {
	archive := writeTestArchive(t, Manifest{Version: "9.9.9", SchemaVersion: 40}, "")
	stage := t.TempDir()
	_, err := stageArchive(bytes.NewReader(archive), stage, 39)
	if err == nil || !strings.Contains(err.Error(), "newer HomeHub") {
		t.Fatalf("want a newer-schema refusal, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(stage, dumpEntry)); !os.IsNotExist(statErr) {
		t.Fatal("dump was extracted from a refused archive")
	}
}

func TestArchiveEntriesCannotChooseWhereTheyLand(t *testing.T) {
	manifest, _ := json.Marshal(Manifest{Workspace: true})
	for _, bad := range []tar.Header{
		{Name: "workspace/../../escape", Typeflag: tar.TypeReg},
		{Name: "workspace/" + uuid.NewString(), Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"},
		{Name: "workspace/not-an-id", Typeflag: tar.TypeReg},
	} {
		var buf bytes.Buffer
		tw := tar.NewWriter(&buf)
		_ = tw.WriteHeader(&tar.Header{Name: "manifest.json", Size: int64(len(manifest)), Mode: 0o640})
		_, _ = tw.Write(manifest)
		_ = tw.WriteHeader(&tar.Header{Name: "database.pgdump", Size: 1, Mode: 0o640})
		_, _ = tw.Write([]byte("x"))
		bad.Mode = 0o640
		_ = tw.WriteHeader(&bad)
		_ = tw.Close()

		stage := t.TempDir()
		if _, err := stageArchive(&buf, stage, 39); err == nil {
			t.Errorf("entry %q (type %c) accepted", bad.Name, bad.Typeflag)
		}
	}
}

func TestDataBeforeManifestIsRefused(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	_ = tw.WriteHeader(&tar.Header{Name: "database.pgdump", Size: 1, Mode: 0o640})
	_, _ = tw.Write([]byte("x"))
	_ = tw.Close()
	if _, err := stageArchive(&buf, t.TempDir(), 39); err == nil {
		t.Fatal("archive without a leading manifest accepted")
	}
}

func TestEncryptedArchiveRoundTrip(t *testing.T) {
	ctx := context.Background()
	svc := testService(&fakeSecrets{stored: map[string][]byte{}})
	identity, err := svc.encryptionIdentity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	plain := writeTestArchive(t, Manifest{SchemaVersion: 1}, "")
	var sealed bytes.Buffer
	w, _ := age.Encrypt(&sealed, identity.Recipient())
	_, _ = w.Write(plain)
	_ = w.Close()

	stream, err := svc.decryptStream(ctx, &sealed, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stageArchive(stream, t.TempDir(), 39); err != nil {
		t.Fatalf("stage decrypted: %v", err)
	}
}
