//go:build linux

package convert

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// The namespace sandbox: how converters stay contained when HomeHub is not
// root (the Docker image, a dedicated service user), where the setpriv drop
// to nobody is impossible.
//
// Every tool is started as a re-exec of this binary inside fresh user, mount,
// network, PID, IPC and UTS namespaces. That child (sandboxChild) builds a new
// root on a tmpfs containing only what a converter needs — /usr, /bin, /lib*,
// /etc read-only, a handful of /dev nodes, its own /proc, and the batch's
// working directory as /work — pivots into it, drops every capability, and
// execs the tool. The data dir (master.key, workspace blobs, other batches),
// /run (the Postgres and Docker sockets, which would accept the app user's
// peer credentials), /home and the rest of the host simply do not exist in
// there, and with no network namespace peers there is nothing to connect to.
//
// This is the kernel feature bubblewrap is built on; doing it natively means
// no extra package to install and the same code path in the container and on
// a host. It needs unprivileged user namespaces (on by default on current
// Debian/Ubuntu/Fedora kernels; blocked by Docker's default seccomp profile).
// When they are unavailable the runner refuses external-tool conversions
// unless the owner explicitly opts out with HOMEHUB_CONVERT_UNSANDBOXED=1.

// sandboxChildEnv marks a process as the sandbox child. It is only ever set
// by sandboxCommand and is stripped before the tool is exec'd.
const sandboxChildEnv = "HOMEHUB_CONVERT_SANDBOX_CHILD"

// sandboxExitSetup is the child's exit status when the sandbox could not be
// built; nothing untrusted has run at that point.
const sandboxExitSetup = 125

func init() {
	if os.Getenv(sandboxChildEnv) != "1" {
		return
	}
	// Capabilities and no_new_privs are per thread: set them on the thread
	// that will call execve, and keep the runtime from moving us off it.
	runtime.LockOSThread()
	os.Exit(sandboxChild(os.Args[1:]))
}

// sandboxCommand wraps argv (absolute tool path first) so it runs inside the
// namespace sandbox with workDir as its /work and cwd.
func sandboxCommand(workDir string, argv []string, env []string) *exec.Cmd {
	args := append([]string{"--work", workDir, "--"}, argv...)
	cmd := exec.Command("/proc/self/exe", args...)
	cmd.Dir = workDir
	cmd.Env = append(append([]string{}, env...), sandboxChildEnv+"=1")
	uid, gid := os.Getuid(), os.Getgid()
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
		Cloneflags: syscall.CLONE_NEWUSER | syscall.CLONE_NEWNS | syscall.CLONE_NEWNET |
			syscall.CLONE_NEWPID | syscall.CLONE_NEWIPC | syscall.CLONE_NEWUTS,
		// The app's own uid becomes root inside the namespace — the only
		// mapping an unprivileged process may write — so the child can build
		// its mounts. It gives up every capability before the tool runs.
		UidMappings:                []syscall.SysProcIDMap{{ContainerID: 0, HostID: uid, Size: 1}},
		GidMappings:                []syscall.SysProcIDMap{{ContainerID: 0, HostID: gid, Size: 1}},
		GidMappingsEnableSetgroups: false,
	}
	return cmd
}

// probeNamespaceSandbox runs `true` through the full sandbox once. Only an
// end-to-end run proves the kernel, seccomp policy and LSM all allow it.
func probeNamespaceSandbox() error {
	truePath, err := exec.LookPath("true")
	if err != nil {
		return fmt.Errorf("no `true` binary to probe with: %w", err)
	}
	dir, err := os.MkdirTemp("", "homehub-sandbox-probe-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := sandboxCommand(dir, []string{truePath}, []string{"PATH=/usr/bin:/bin"})
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("user namespaces unavailable: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err = <-done:
	case <-ctx.Done():
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
		return errors.New("sandbox probe timed out")
	}
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return errors.New(oneLine(msg))
		}
		return err
	}
	return nil
}

func sandboxChild(args []string) int {
	if len(args) < 4 || args[0] != "--work" || args[2] != "--" {
		fmt.Fprintln(os.Stderr, "convert sandbox: bad invocation")
		return sandboxExitSetup
	}
	work, argv := args[1], args[3:]
	if err := enterSandbox(work); err != nil {
		fmt.Fprintln(os.Stderr, "convert sandbox:", err)
		return sandboxExitSetup
	}
	env := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, sandboxChildEnv+"=") {
			env = append(env, kv)
		}
	}
	err := unix.Exec(argv[0], argv, env)
	fmt.Fprintf(os.Stderr, "convert sandbox: exec %s: %v\n", argv[0], err)
	return 126
}

// hostDirs are the only parts of the host visible in the sandbox, all
// read-only: programs, libraries and their configuration. Deliberately not
// /opt, /srv, /home, /root, /var or /run — the data dir and the sockets live
// there.
var hostDirs = []string{"/usr", "/bin", "/sbin", "/lib", "/lib32", "/lib64", "/libx32", "/etc"}

func enterSandbox(work string) error {
	// Hold the work dir by descriptor: the new root is built on /tmp, which
	// may be an ancestor of it (tests, or a data dir under /tmp).
	workFD, err := unix.Open(work, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open work dir: %w", err)
	}
	defer unix.Close(workFD)
	workSrc := fmt.Sprintf("/proc/self/fd/%d", workFD)

	// Nothing done here may propagate back to the host's mount table.
	if err := unix.Mount("", "/", "", unix.MS_REC|unix.MS_PRIVATE, ""); err != nil {
		return fmt.Errorf("make mounts private: %w", err)
	}
	const root = "/tmp"
	rootFlags := uintptr(unix.MS_NOSUID | unix.MS_NODEV)
	if err := unix.Mount("tmpfs", root, "tmpfs", rootFlags, "size=1m,mode=0755"); err != nil {
		return fmt.Errorf("mount new root: %w", err)
	}

	for _, d := range hostDirs {
		if err := bindHostDir(root, d); err != nil {
			return err
		}
	}

	if err := os.Mkdir(root+"/dev", 0o755); err != nil {
		return err
	}
	for _, n := range []string{"null", "zero", "full", "random", "urandom"} {
		if err := bindFile("/dev/"+n, root+"/dev/"+n); err != nil {
			return err
		}
	}
	for name, target := range map[string]string{
		"fd": "/proc/self/fd", "stdin": "/proc/self/fd/0", "stdout": "/proc/self/fd/1", "stderr": "/proc/self/fd/2",
	} {
		if err := os.Symlink(target, root+"/dev/"+name); err != nil {
			return err
		}
	}

	// A /proc of our own PID namespace: the tool sees itself and nothing
	// else (in particular not HomeHub's environment, which can carry the
	// database URL and master key). Best effort — some kernels refuse a
	// fresh procfs when the host's is partly masked, and the tools run
	// without one.
	if err := os.Mkdir(root+"/proc", 0o555); err != nil {
		return err
	}
	_ = unix.Mount("proc", root+"/proc", "proc", unix.MS_NOSUID|unix.MS_NODEV|unix.MS_NOEXEC, "")

	if err := os.Mkdir(root+"/work", 0o755); err != nil {
		return err
	}
	if err := unix.Mount(workSrc, root+"/work", "", unix.MS_BIND|unix.MS_REC, ""); err != nil {
		return fmt.Errorf("bind work dir: %w", err)
	}
	if err := remount(root+"/work", unix.MS_NOSUID|unix.MS_NODEV); err != nil {
		return err
	}

	// Scratch space lands in the batch's own tmp dir (on disk, removed with
	// the batch) when there is one, otherwise in a bounded tmpfs.
	if err := os.Mkdir(root+"/tmp", 0o1777); err != nil {
		return err
	}
	if fi, err := os.Stat(root + "/work/tmp"); err == nil && fi.IsDir() {
		if err := unix.Mount(root+"/work/tmp", root+"/tmp", "", unix.MS_BIND, ""); err != nil {
			return fmt.Errorf("bind tmp: %w", err)
		}
	} else if err := unix.Mount("tmpfs", root+"/tmp", "tmpfs", unix.MS_NOSUID|unix.MS_NODEV, "size=512m,mode=1777"); err != nil {
		return fmt.Errorf("mount tmp: %w", err)
	}

	if err := os.Mkdir(root+"/.old", 0o700); err != nil {
		return err
	}
	if err := unix.PivotRoot(root, root+"/.old"); err != nil {
		return fmt.Errorf("pivot_root: %w", err)
	}
	if err := unix.Chdir("/"); err != nil {
		return err
	}
	if err := unix.Unmount("/.old", unix.MNT_DETACH); err != nil {
		return fmt.Errorf("detach host root: %w", err)
	}
	_ = os.Remove("/.old")
	if err := unix.Mount("", "/", "", unix.MS_REMOUNT|unix.MS_RDONLY|rootFlags, ""); err != nil {
		return fmt.Errorf("seal new root: %w", err)
	}
	if err := unix.Chdir("/work"); err != nil {
		return err
	}
	return dropCapabilities()
}

func bindHostDir(root, dir string) error {
	fi, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	target := filepath.Join(root, dir)
	if fi.Mode()&os.ModeSymlink != 0 {
		// Merged-/usr hosts: /bin -> usr/bin and friends.
		link, err := os.Readlink(dir)
		if err != nil {
			return err
		}
		return os.Symlink(link, target)
	}
	if !fi.IsDir() {
		return nil
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	if err := unix.Mount(dir, target, "", unix.MS_BIND|unix.MS_REC, ""); err != nil {
		return fmt.Errorf("bind %s: %w", dir, err)
	}
	return remount(target, unix.MS_RDONLY|unix.MS_NOSUID|unix.MS_NODEV)
}

// remount re-applies a bind mount with extra flags. Flags the host mount
// already carries (noexec, nosuid, nodev, rdonly) are locked inside a user
// namespace and must be repeated or the kernel refuses the remount.
func remount(target string, add uintptr) error {
	var st unix.Statfs_t
	if err := unix.Statfs(target, &st); err != nil {
		return fmt.Errorf("statfs %s: %w", target, err)
	}
	flags := uintptr(unix.MS_BIND|unix.MS_REMOUNT) | add
	for st0, ms := range map[int64]uintptr{
		unix.ST_RDONLY: unix.MS_RDONLY, unix.ST_NOSUID: unix.MS_NOSUID,
		unix.ST_NODEV: unix.MS_NODEV, unix.ST_NOEXEC: unix.MS_NOEXEC,
	} {
		if int64(st.Flags)&st0 != 0 {
			flags |= ms
		}
	}
	if err := unix.Mount("", target, "", flags, ""); err != nil {
		return fmt.Errorf("remount %s: %w", target, err)
	}
	return nil
}

func bindFile(src, dst string) error {
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY, 0o666)
	if err != nil {
		return err
	}
	_ = f.Close()
	if err := unix.Mount(src, dst, "", unix.MS_BIND, ""); err != nil {
		return fmt.Errorf("bind %s: %w", src, err)
	}
	return nil
}

// Securebits (linux/securebits.h): never let uid 0 in here regain
// capabilities through execve, and lock that in.
const (
	secbitNoroot             = 1 << 0
	secbitNorootLocked       = 1 << 1
	secbitNoSetuidFixup      = 1 << 2
	secbitNoSetuidFixupLockd = 1 << 3
	secbitKeepCapsLocked     = 1 << 5
)

// dropCapabilities leaves the tool with no capabilities at all, even inside
// its own namespace, and with no way to gain any.
func dropCapabilities() error {
	for c := 0; c <= 63; c++ {
		if err := unix.Prctl(unix.PR_CAPBSET_DROP, uintptr(c), 0, 0, 0); err != nil {
			if errors.Is(err, unix.EINVAL) {
				break // past the last capability this kernel knows
			}
			return fmt.Errorf("drop bounding capability %d: %w", c, err)
		}
	}
	if err := unix.Prctl(unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_CLEAR_ALL, 0, 0, 0); err != nil && !errors.Is(err, unix.EINVAL) {
		return fmt.Errorf("clear ambient capabilities: %w", err)
	}
	bits := secbitNoroot | secbitNorootLocked | secbitNoSetuidFixup | secbitNoSetuidFixupLockd | secbitKeepCapsLocked
	if err := unix.Prctl(unix.PR_SET_SECUREBITS, uintptr(bits), 0, 0, 0); err != nil {
		return fmt.Errorf("set securebits: %w", err)
	}
	hdr := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	var data [2]unix.CapUserData
	if err := unix.Capset(&hdr, &data[0]); err != nil {
		return fmt.Errorf("clear capabilities: %w", err)
	}
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("set no_new_privs: %w", err)
	}
	return nil
}
