package backup

import (
	"context"

	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Live round-trips against real servers. Enabled with HOMEHUB_LIVE_TARGETS=1
// (the dev sandbox runs Samba on :445 and MinIO on :9000); skipped otherwise
// so the suite stays hermetic.

func requireLive(t *testing.T) {
	t.Helper()
	if os.Getenv("HOMEHUB_LIVE_TARGETS") != "1" {
		t.Skip("set HOMEHUB_LIVE_TARGETS=1 with local Samba/MinIO to run")
	}
}

func roundtrip(t *testing.T, target Target) {
	t.Helper()
	ctx := context.Background()
	defer target.Close()

	if err := target.Test(ctx); err != nil {
		t.Fatalf("test: %v", err)
	}
	payload := "backup archive payload " + time.Now().String()
	if err := target.Write(ctx, "homehub/app-backup-test.tar", strings.NewReader(payload), int64(len(payload))); err != nil {
		t.Fatalf("write: %v", err)
	}
	reader, err := target.Read(ctx, "homehub/app-backup-test.tar")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	got, _ := io.ReadAll(reader)
	reader.Close()
	if string(got) != payload {
		t.Fatal("read-back mismatch")
	}
	entries, err := target.List(ctx, "homehub/")
	if err != nil || len(entries) == 0 {
		t.Fatalf("list: %d entries, err %v", len(entries), err)
	}
	if err := target.Delete(ctx, "homehub/app-backup-test.tar"); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

func TestLocalTarget(t *testing.T) {
	dir := t.TempDir()
	target, err := Open(context.Background(), "local", map[string]any{"path": dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	roundtrip(t, target)
}

func TestSMBTargetLive(t *testing.T) {
	requireLive(t)
	target, err := Open(context.Background(), "smb", map[string]any{
		"host": "127.0.0.1", "share": "backups", "subdir": "homehub-sub",
	}, map[string]string{"username": "backupuser", "password": "smb-test-pw"})
	if err != nil {
		t.Fatal(err)
	}
	roundtrip(t, target)

	_, err = Open(context.Background(), "smb", map[string]any{"host": "127.0.0.1", "share": "backups"},
		map[string]string{"username": "backupuser", "password": "wrong"})
	if err == nil || !strings.Contains(err.Error(), "authentication failed") {
		t.Fatalf("expected friendly auth error, got %v", err)
	}
}

func TestS3TargetLive(t *testing.T) {
	requireLive(t)
	ctx := context.Background()
	admin, err := minio.New("127.0.0.1:9000", &minio.Options{
		Creds: credentials.NewStaticV4("minioadmin", "minioadmin", ""), Secure: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = admin.MakeBucket(ctx, "homehub-backups", minio.MakeBucketOptions{})

	target, err := Open(ctx, "s3", map[string]any{
		"endpoint": "http://127.0.0.1:9000", "bucket": "homehub-backups", "prefix": "lab",
	}, map[string]string{"accessKey": "minioadmin", "secretKey": "minioadmin"})
	if err != nil {
		t.Fatal(err)
	}
	roundtrip(t, target)

	_, err = Open(ctx, "s3", map[string]any{"endpoint": "http://127.0.0.1:9000", "bucket": "does-not-exist"},
		map[string]string{"accessKey": "minioadmin", "secretKey": "minioadmin"})
	if err == nil {
		if terr := func() error {
			bad, _ := Open(ctx, "s3", map[string]any{"endpoint": "http://127.0.0.1:9000", "bucket": "does-not-exist"},
				map[string]string{"accessKey": "minioadmin", "secretKey": "minioadmin"})
			defer bad.Close()
			return bad.Test(ctx)
		}(); terr == nil || !strings.Contains(terr.Error(), "does not exist") {
			t.Fatalf("expected missing-bucket error, got %v", terr)
		}
	}
}
