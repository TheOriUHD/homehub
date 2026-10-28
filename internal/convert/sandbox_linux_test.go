//go:build linux

package convert

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// sandboxProbeScript runs as the "tool" inside the sandbox. Each check exits
// with its own code so a failure names what leaked.
const sandboxProbeScript = `
test "$(cat in/source.txt)" = hello || exit 10
cat "$SECRET" 2>/dev/null && exit 11
ls "$DATADIR" 2>/dev/null && exit 12
test -e /run && exit 13
test -e /home && exit 13
echo converted > out/result.txt || exit 14
touch /usr/sandbox-write-test 2>/dev/null && exit 15
touch /etc/sandbox-write-test 2>/dev/null && exit 15
if test -r /proc/net/dev; then
	test "$(grep -c : /proc/net/dev)" = 1 || exit 16
	grep -q '^CapEff:[[:space:]]*0*$' /proc/self/status || exit 17
	test "$(ls /proc | grep -c '^[0-9]')" -le 4 || exit 18
fi
exit 0
`

var sandboxProbeFailures = map[int]string{
	10: "the tool could not read its own input",
	11: "the tool read master.key",
	12: "the tool could list the data dir",
	13: "the host's /run or /home is visible",
	14: "the tool could not write its output",
	15: "the host's /usr or /etc is writable",
	16: "the tool has a network interface besides loopback",
	17: "the tool kept capabilities",
	18: "the tool can see processes outside its sandbox",
}

func runSandboxProbe(t *testing.T) {
	t.Helper()
	if err := probeNamespaceSandbox(); err != nil {
		t.Skipf("namespace sandbox unavailable here: %v", err)
	}
	dataDir := t.TempDir()
	secret := filepath.Join(dataDir, "master.key")
	if err := os.WriteFile(secret, []byte("TOP-SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(dataDir, "convert", "batch")
	for _, d := range []string{"in", "out", "tmp"} {
		if err := os.MkdirAll(filepath.Join(work, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(work, "in", "source.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	cmd := sandboxCommand(work, []string{sh, "-c", sandboxProbeScript},
		[]string{"PATH=/usr/bin:/bin", "SECRET=" + secret, "DATADIR=" + dataDir})
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = io.Discard
	err = cmd.Run()
	if ee, ok := err.(*exec.ExitError); ok {
		code := ee.ExitCode()
		if why, known := sandboxProbeFailures[code]; known {
			t.Fatalf("sandbox leak: %s (stderr: %s)", why, stderr.String())
		}
		t.Fatalf("sandbox probe exited %d: %s", code, stderr.String())
	} else if err != nil {
		t.Fatalf("sandbox probe: %v", err)
	}
	out, err := os.ReadFile(filepath.Join(work, "out", "result.txt"))
	if err != nil || strings.TrimSpace(string(out)) != "converted" {
		t.Fatalf("output did not reach the batch dir: %q %v", out, err)
	}
}

// The sandbox hides the data dir, the host's sockets and network, and leaves
// the tool no capabilities, while its batch dir stays usable.
func TestNamespaceSandboxContainsTheTool(t *testing.T) {
	runSandboxProbe(t)
}

// The same, as an ordinary unprivileged user — the case the sandbox exists
// for (Docker image, service user). When the suite runs as root the test
// re-runs itself as nobody from a world-readable copy of the test binary.
func TestNamespaceSandboxAsUnprivilegedUser(t *testing.T) {
	if os.Getenv("HOMEHUB_TEST_UNPRIVILEGED") == "1" || os.Geteuid() != 0 {
		runSandboxProbe(t)
		return
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp("", "homehub-sandbox-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "convert.test")
	src, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, src, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "-test.run", "^TestNamespaceSandboxAsUnprivilegedUser$", "-test.v")
	cmd.Dir = dir
	cmd.Env = []string{"HOMEHUB_TEST_UNPRIVILEGED=1", "PATH=/usr/bin:/bin", "TMPDIR=/tmp"}
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 65534, Gid: 65534}}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("unprivileged run failed: %v\n%s", err, out)
	}
	if strings.Contains(string(out), "--- SKIP") {
		t.Skipf("unprivileged user namespaces unavailable here:\n%s", out)
	}
	t.Logf("%s", out)
}

// With no sandbox mechanism and no opt-out, external tools are refused; the
// opt-out is honoured and reported as such.
func TestNoSandboxFailsClosedUnlessOptedOut(t *testing.T) {
	r := &Runner{mode: SandboxUnavailable, brokenReason: "no sandbox here", logger: slog.Default(), tools: map[string]string{}}
	e, _ := EdgeFor("png", "jpg")
	if err := r.Convert(t.Context(), e, t.TempDir(), "in.png", "out.jpg", ".", "item", Options{}); err == nil ||
		!strings.Contains(err.Error(), "no sandbox here") {
		t.Fatalf("convert without a sandbox: %v, want refusal", err)
	}
	caps := &Caps{SandboxBroken: true, SandboxReason: "no sandbox here", ImageMagick: true}
	if ok, why := caps.Available(e); ok || why != "no sandbox here" {
		t.Fatalf("edge available=%v reason=%q", ok, why)
	}
	tableEdge, ok := EdgeFor("csv", "json")
	if ok {
		if avail, _ := caps.Available(tableEdge); !avail {
			t.Fatal("in-process table conversions must not need the sandbox")
		}
	}
}
