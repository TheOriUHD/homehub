package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/hirochachacha/go-smb2"
)

// smbTarget talks SMB2/3 directly in userspace — host, share, credentials,
// optional subfolder. No mount, no privileges.
type smbTarget struct {
	conn    net.Conn
	session *smb2.Session
	share   *smb2.Share
	subdir  string
}

func openSMB(ctx context.Context, config map[string]any, secrets map[string]string) (Target, error) {
	host := cfgString(config, "host")
	shareName := cfgString(config, "share")
	if host == "" || shareName == "" {
		return nil, errors.New("host and share name are required")
	}
	if !strings.Contains(host, ":") {
		host += ":445"
	}
	username := secrets["username"]
	if username == "" {
		return nil, errors.New("an SMB username is required (use 'guest' for guest shares)")
	}

	dialer := net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", host)
	if err != nil {
		return nil, fmt.Errorf("cannot reach %s: %w", host, err)
	}
	domain := cfgString(config, "domain")
	smbDialer := &smb2.Dialer{
		Initiator: &smb2.NTLMInitiator{
			User:     username,
			Password: secrets["password"],
			Domain:   domain,
		},
	}
	session, err := smbDialer.DialContext(ctx, conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("SMB authentication failed — check username, password, and domain: %w", err)
	}
	share, err := session.Mount(shareName)
	if err != nil {
		session.Logoff()
		conn.Close()
		return nil, fmt.Errorf("cannot open share %q — check the share name and its permissions: %w", shareName, err)
	}
	subdir := strings.Trim(cfgString(config, "subdir"), "/")
	if subdir != "" {
		if err := share.MkdirAll(toSMBPath(subdir), 0o750); err != nil && !os.IsExist(err) {
			session.Logoff()
			conn.Close()
			return nil, fmt.Errorf("cannot create subfolder %q on the share: %w", subdir, err)
		}
	}
	return &smbTarget{conn: conn, session: session, share: share, subdir: subdir}, nil
}

// toSMBPath converts slash paths to the backslash separators go-smb2 expects.
func toSMBPath(p string) string {
	return strings.ReplaceAll(p, "/", `\`)
}

func (t *smbTarget) full(p string) string {
	if t.subdir == "" {
		return toSMBPath(p)
	}
	return toSMBPath(path.Join(t.subdir, p))
}

func (t *smbTarget) Close() {
	_ = t.share.Umount()
	_ = t.session.Logoff()
	_ = t.conn.Close()
}

func (t *smbTarget) Test(ctx context.Context) error { return runProbe(ctx, t) }

func (t *smbTarget) Write(_ context.Context, p string, r io.Reader, _ int64) error {
	full := t.full(p)
	if dir := path.Dir(strings.ReplaceAll(full, `\`, "/")); dir != "." && dir != "/" {
		_ = t.share.MkdirAll(toSMBPath(dir), 0o750)
	}
	tmp := full + ".partial"
	file, err := t.share.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(file, r); err != nil {
		file.Close()
		_ = t.share.Remove(tmp)
		return err
	}
	if err := file.Close(); err != nil {
		_ = t.share.Remove(tmp)
		return err
	}
	_ = t.share.Remove(full) // rename fails on existing name on some servers
	return t.share.Rename(tmp, full)
}

func (t *smbTarget) Read(_ context.Context, p string) (io.ReadCloser, error) {
	return t.share.Open(t.full(p))
}

func (t *smbTarget) List(_ context.Context, prefix string) ([]Entry, error) {
	entries := []Entry{}
	var walk func(dir string) error
	walk = func(dir string) error {
		items, err := t.share.ReadDir(toSMBPath(dir))
		if err != nil {
			return err
		}
		for _, item := range items {
			rel := path.Join(dir, item.Name())
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

func (t *smbTarget) Delete(_ context.Context, p string) error {
	return t.share.Remove(t.full(p))
}

func (t *smbTarget) Capacity(context.Context) (Capacity, error) {
	stat, err := t.share.Statfs(".")
	if err != nil {
		return Capacity{Unknown: true}, nil
	}
	return Capacity{
		TotalBytes: int64(stat.TotalBlockCount() * stat.BlockSize()),
		FreeBytes:  int64(stat.AvailableBlockCount() * stat.BlockSize()),
	}, nil
}
