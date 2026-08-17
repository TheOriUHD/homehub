package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/vmware/go-nfs-client/nfs"
	"github.com/vmware/go-nfs-client/nfs/rpc"
)

// nfsTarget speaks NFSv3 in userspace (no kernel mount). The export must
// allow the connecting UID; AUTH_UNIX with a configurable uid/gid is used,
// matching how appliance exports are typically opened up.
type nfsTarget struct {
	mount  *nfs.Mount
	target *nfs.Target
	subdir string
}

func openNFS(_ context.Context, config map[string]any) (Target, error) {
	host := cfgString(config, "host")
	export := cfgString(config, "export")
	if host == "" || export == "" {
		return nil, errors.New("host and export path are required, e.g. nas.lan and /mnt/tank/backups")
	}
	uid, gid := uint32(0), uint32(0)
	if v, ok := config["uid"].(float64); ok {
		uid = uint32(v)
	}
	if v, ok := config["gid"].(float64); ok {
		gid = uint32(v)
	}

	mount, err := nfs.DialMount(host)
	if err != nil {
		return nil, fmt.Errorf("cannot reach the NFS server at %s (portmapper/mountd): %w", host, err)
	}
	auth := rpc.NewAuthUnix("homehub", uid, gid)
	target, err := mount.Mount(export, auth.Auth())
	if err != nil {
		mount.Close()
		return nil, fmt.Errorf("cannot mount export %q — check the export path and that this host is allowed: %w", export, err)
	}
	t := &nfsTarget{mount: mount, target: target, subdir: strings.Trim(cfgString(config, "subdir"), "/")}
	if t.subdir != "" {
		if _, err := target.Mkdir(t.subdir, 0o750); err != nil && !os.IsExist(err) {
			// Existing directories are fine; report anything else.
			if _, _, lookupErr := target.Lookup(t.subdir); lookupErr != nil {
				t.Close()
				return nil, fmt.Errorf("cannot create subfolder %q on the export: %w", t.subdir, err)
			}
		}
	}
	return t, nil
}

func (t *nfsTarget) Close() {
	_ = t.target.Close()
	_ = t.mount.Close()
}

func (t *nfsTarget) full(p string) string {
	if t.subdir == "" {
		return p
	}
	return path.Join(t.subdir, p)
}

func (t *nfsTarget) Test(ctx context.Context) error { return runProbe(ctx, t) }

func (t *nfsTarget) Write(_ context.Context, p string, r io.Reader, _ int64) error {
	full := t.full(p)
	if dir := path.Dir(full); dir != "." {
		mkdirAllNFS(t.target, dir)
	}
	// The NFSv3 client library has no RENAME op, so writes go straight to
	// the final name and are removed on failure instead of atomically
	// swapped.
	_ = t.target.Remove(full)
	file, err := t.target.OpenFile(full, 0o640)
	if err != nil {
		return err
	}
	if _, err := io.Copy(file, r); err != nil {
		file.Close()
		_ = t.target.Remove(full)
		return err
	}
	if err := file.Close(); err != nil {
		_ = t.target.Remove(full)
		return err
	}
	return nil
}

func mkdirAllNFS(target *nfs.Target, dir string) {
	parts := strings.Split(dir, "/")
	current := ""
	for _, part := range parts {
		current = path.Join(current, part)
		_, _ = target.Mkdir(current, 0o750)
	}
}

func (t *nfsTarget) Read(_ context.Context, p string) (io.ReadCloser, error) {
	return t.target.Open(t.full(p))
}

func (t *nfsTarget) List(_ context.Context, prefix string) ([]Entry, error) {
	entries := []Entry{}
	var walk func(dir string) error
	walk = func(dir string) error {
		items, err := t.target.ReadDirPlus(dir)
		if err != nil {
			return err
		}
		for _, item := range items {
			name := item.FileName
			if name == "." || name == ".." {
				continue
			}
			rel := path.Join(dir, name)
			if item.IsDir() {
				if err := walk(rel); err != nil {
					return err
				}
				continue
			}
			clean := rel
			if t.subdir != "" {
				clean = strings.TrimPrefix(strings.TrimPrefix(rel, t.subdir), "/")
			}
			clean = strings.TrimPrefix(clean, "./")
			if prefix != "" && !strings.HasPrefix(clean, prefix) {
				continue
			}
			if strings.HasSuffix(clean, ".partial") {
				continue
			}
			entries = append(entries, Entry{Path: clean, Size: item.Size(), ModTime: item.ModTime()})
		}
		return nil
	}
	root := "."
	if t.subdir != "" {
		root = t.subdir
	}
	if err := walk(root); err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ModTime.After(entries[j].ModTime) })
	return entries, nil
}

func (t *nfsTarget) Delete(_ context.Context, p string) error {
	return t.target.Remove(t.full(p))
}

func (t *nfsTarget) Capacity(context.Context) (Capacity, error) {
	fsinfo, err := t.target.FSInfo()
	if err != nil {
		return Capacity{Unknown: true}, nil
	}
	_ = fsinfo
	// go-nfs-client's FSINFO does not expose FSSTAT space fields; report
	// unknown rather than a wrong number.
	return Capacity{Unknown: true}, nil
}
