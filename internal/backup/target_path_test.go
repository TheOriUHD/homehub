package backup

import (
	"context"
	"strings"
	"testing"
)

var escapingPaths = []string{
	"../outside.tar",
	"homehub/../../outside.tar",
	"homehub/..",
	"..",
	"/etc/passwd",
	`homehub\..\outside.tar`,
	"homehub/\x00.tar",
	"",
	".",
}

func TestObjectPathsCannotLeaveTheTarget(t *testing.T) {
	for _, p := range escapingPaths {
		if got, err := cleanObjectPath(p); err == nil {
			t.Errorf("cleanObjectPath(%q) = %q, want a refusal", p, got)
		}
	}
	for p, want := range map[string]string{
		"homehub/app-backup-20260101-120000.tar.age": "homehub/app-backup-20260101-120000.tar.age",
		".homehub-write-test":                        ".homehub-write-test",
		"homehub//./a.tar":                           "homehub/a.tar",
	} {
		if got, err := cleanObjectPath(p); err != nil || got != want {
			t.Errorf("cleanObjectPath(%q) = %q, %v; want %q", p, got, err, want)
		}
	}
}

func TestConfiguredSubfolderCannotLeaveTheShare(t *testing.T) {
	for _, raw := range []string{"../other", "backups/../../x", "a/..", `a\..\b`} {
		if got, err := cleanSubdir("subfolder", raw); err == nil {
			t.Errorf("cleanSubdir(%q) = %q, want a refusal", raw, got)
		}
	}
	// Slashes around the folder are how people type it; they stay accepted.
	for raw, want := range map[string]string{"": "", "/": "", "/backups/homehub/": "backups/homehub"} {
		if got, err := cleanSubdir("subfolder", raw); err != nil || got != want {
			t.Errorf("cleanSubdir(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}
}

// The traversal checks run before anything is dialed, so a bad config is
// refused on save without a network round-trip.
func TestTargetsRefuseEscapingConfig(t *testing.T) {
	ctx := context.Background()
	if _, err := openSMB(ctx, map[string]any{"host": "192.0.2.1", "share": "b", "subdir": "../x"},
		map[string]string{"username": "u"}); err == nil || !strings.Contains(err.Error(), "subfolder") {
		t.Errorf("smb: %v", err)
	}
	if _, err := openNFS(ctx, map[string]any{"host": "192.0.2.1", "export": "/e", "subdir": "a/../../x"}); err == nil || !strings.Contains(err.Error(), "subfolder") {
		t.Errorf("nfs: %v", err)
	}
	if _, err := openS3(map[string]any{"endpoint": "s3.example", "bucket": "b", "prefix": "../x"},
		map[string]string{"accessKey": "a", "secretKey": "s"}); err == nil || !strings.Contains(err.Error(), "prefix") {
		t.Errorf("s3: %v", err)
	}
}

func TestTargetsRefuseEscapingObjectPaths(t *testing.T) {
	smb := &smbTarget{subdir: "backups"}
	nfs := &nfsTarget{subdir: "backups"}
	s3 := &s3Target{prefix: "backups"}
	for _, p := range escapingPaths {
		if got, err := smb.full(p); err == nil {
			t.Errorf("smb.full(%q) = %q", p, got)
		}
		if got, err := nfs.full(p); err == nil {
			t.Errorf("nfs.full(%q) = %q", p, got)
		}
		if got, err := s3.full(p); err == nil {
			t.Errorf("s3.full(%q) = %q", p, got)
		}
	}
	if got, _ := smb.full("homehub/a.tar"); got != `backups\homehub\a.tar` {
		t.Errorf("smb.full = %q", got)
	}
	if got, _ := s3.full("homehub/a.tar"); got != "backups/homehub/a.tar" {
		t.Errorf("s3.full = %q", got)
	}
}
