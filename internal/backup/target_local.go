package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/sys/unix"
)

// localTarget stores archives under a directory — a disk path or an
// already-mounted network filesystem.
type localTarget struct {
	root string
}

func openLocal(config map[string]any) (Target, error) {
	root := cfgString(config, "path")
	if root == "" {
		return nil, errors.New("a directory path is required")
	}
	if !filepath.IsAbs(root) {
		return nil, errors.New("the path must be absolute")
	}
	return &localTarget{root: root}, nil
}

func (t *localTarget) Close() {}

// resolve joins and confines a relative object path to the root.
func (t *localTarget) resolve(path string) (string, error) {
	joined := filepath.Join(t.root, filepath.FromSlash(path))
	if !strings.HasPrefix(joined, filepath.Clean(t.root)+string(os.PathSeparator)) && joined != filepath.Clean(t.root) {
		return "", errors.New("path escapes the target directory")
	}
	return joined, nil
}

func (t *localTarget) Test(ctx context.Context) error {
	info, err := os.Stat(t.root)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("directory %s does not exist — create it (or mount the share) first", t.root)
		}
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", t.root)
	}
	return runProbe(ctx, t)
}

func (t *localTarget) Write(_ context.Context, path string, r io.Reader, _ int64) error {
	full, err := t.resolve(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		return err
	}
	tmp := full + ".partial"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return err
	}
	if _, err := io.Copy(file, r); err != nil {
		file.Close()
		os.Remove(tmp)
		return err
	}
	if err := file.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, full)
}

func (t *localTarget) Read(_ context.Context, path string) (io.ReadCloser, error) {
	full, err := t.resolve(path)
	if err != nil {
		return nil, err
	}
	return os.Open(full)
}

func (t *localTarget) List(_ context.Context, prefix string) ([]Entry, error) {
	entries := []Entry{}
	err := filepath.WalkDir(t.root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, rerr := filepath.Rel(t.root, path)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if prefix != "" && !strings.HasPrefix(rel, prefix) {
			return nil
		}
		if strings.HasSuffix(rel, ".partial") {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		entries = append(entries, Entry{Path: rel, Size: info.Size(), ModTime: info.ModTime()})
		return nil
	})
	sort.Slice(entries, func(i, j int) bool { return entries[i].ModTime.After(entries[j].ModTime) })
	return entries, err
}

func (t *localTarget) Delete(_ context.Context, path string) error {
	full, err := t.resolve(path)
	if err != nil {
		return err
	}
	return os.Remove(full)
}

func (t *localTarget) Capacity(context.Context) (Capacity, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(t.root, &stat); err != nil {
		return Capacity{Unknown: true}, nil
	}
	return Capacity{
		TotalBytes: int64(stat.Blocks) * int64(stat.Bsize),
		FreeBytes:  int64(stat.Bavail) * int64(stat.Bsize),
	}, nil
}
