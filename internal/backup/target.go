// Package backup implements storage targets and the application backup
// lifecycle. Targets are pure userspace clients — SMB and S3 need no kernel
// mounts and no privileged container, so adding or removing a location at
// runtime never touches the host and never interrupts anything.
package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"
)

// Entry is one stored object on a target.
type Entry struct {
	Path    string
	Size    int64
	ModTime time.Time
}

// Capacity describes target space; Unknown is true where the protocol
// cannot report it (S3).
type Capacity struct {
	TotalBytes int64
	FreeBytes  int64
	Unknown    bool
}

// Target is a storage location for backup archives.
type Target interface {
	// Test verifies connectivity and write permission by writing and
	// removing a probe object. The returned error is user-facing.
	Test(ctx context.Context) error
	Write(ctx context.Context, path string, r io.Reader, size int64) error
	Read(ctx context.Context, path string) (io.ReadCloser, error)
	List(ctx context.Context, prefix string) ([]Entry, error)
	Delete(ctx context.Context, path string) error
	Capacity(ctx context.Context) (Capacity, error)
	Close()
}

// TargetTypes lists supported types with their vault secret names.
var TargetTypes = map[string][]string{
	"local": {},
	"smb":   {"username", "password"},
	"nfs":   {},
	"s3":    {"accessKey", "secretKey"},
}

// Open constructs a live target from config + secrets.
func Open(ctx context.Context, typ string, config map[string]any, secrets map[string]string) (Target, error) {
	switch typ {
	case "local":
		return openLocal(config)
	case "smb":
		return openSMB(ctx, config, secrets)
	case "s3":
		return openS3(config, secrets)
	case "nfs":
		return openNFS(ctx, config)
	}
	return nil, fmt.Errorf("unknown target type %q", typ)
}

func cfgString(config map[string]any, key string) string {
	v, _ := config[key].(string)
	return strings.TrimSpace(v)
}

// cleanObjectPath validates a path that a target joins onto its configured
// root: an archive name from the API, or a name the backup service builds. It
// must stay inside the root, so absolute paths, backslashes (a separator on
// SMB), NUL and any ".." segment are refused outright rather than cleaned
// into something that merely looks harmless.
func cleanObjectPath(p string) (string, error) {
	if p == "" {
		return "", errors.New("path is empty")
	}
	if strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("path %q must be relative to the backup location", p)
	}
	if strings.ContainsAny(p, "\\\x00") {
		return "", fmt.Errorf("path %q contains a character that is not allowed", p)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return "", fmt.Errorf("path %q must not contain \"..\"", p)
		}
	}
	clean := path.Clean(p)
	if clean == "." {
		return "", fmt.Errorf("path %q names the backup location itself", p)
	}
	return clean, nil
}

// cleanSubdir validates a configured subfolder or key prefix. Leading and
// trailing slashes are dropped (it is always relative to the share, export or
// bucket); empty means the root itself.
func cleanSubdir(field, raw string) (string, error) {
	trimmed := strings.Trim(raw, "/")
	if trimmed == "" {
		return "", nil
	}
	clean, err := cleanObjectPath(trimmed)
	if err != nil {
		return "", fmt.Errorf("%s: %w", field, err)
	}
	return clean, nil
}

// probeName is the object written and removed by Test.
const probeName = ".homehub-write-test"

// runProbe implements Test generically over a target's primitives.
func runProbe(ctx context.Context, t Target) error {
	payload := []byte("homehub write test " + time.Now().Format(time.RFC3339))
	if err := t.Write(ctx, probeName, strings.NewReader(string(payload)), int64(len(payload))); err != nil {
		return fmt.Errorf("write test failed: %w", err)
	}
	reader, err := t.Read(ctx, probeName)
	if err != nil {
		return fmt.Errorf("read-back test failed: %w", err)
	}
	got, err := io.ReadAll(reader)
	reader.Close()
	if err != nil || string(got) != string(payload) {
		return errors.New("read-back test returned different data")
	}
	if err := t.Delete(ctx, probeName); err != nil {
		return fmt.Errorf("cleanup of test object failed: %w", err)
	}
	return nil
}
