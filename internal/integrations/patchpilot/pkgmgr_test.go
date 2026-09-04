package patchpilot

import (
	"strings"
	"testing"
)

// TestAptSecurityMirrorInjection locks in that the Ubuntu security-mirror
// workaround is wired into the apt commands (and only those) — the fix for
// security.ubuntu.com being Cloudflare-blackholed on some networks.
func TestAptSecurityMirrorInjection(t *testing.T) {
	paths := map[string]string{"apt-get": "/usr/bin/apt-get", "apt": "/usr/bin/apt", "dnf": "/usr/bin/dnf"}

	up := UpdateCommand(MgrApt, paths, "sudo -n ")
	for _, want := range []string{
		"security.ubuntu.com",                 // the probe/detect
		"$PP_SRCOPT",                          // the override token
		"-o Dir::Etc::sourcelist=",            // the transient source override
		"mktemp -d",                           // temp rebuild, not a host edit
		"sudo -n /usr/bin/apt-get $PP_SRCOPT", // applied to the real apt call
		"dist-upgrade",
	} {
		if !contains(up, want) {
			t.Errorf("apt UpdateCommand missing %q", want)
		}
	}
	// The preamble must be present exactly once (update reuses one setup).
	if n := count(up, "routing ${PP_CN}-security"); n != 1 {
		t.Errorf("expected the workaround preamble once, found %d", n)
	}

	dry := DryRunCommand(MgrApt, paths, "sudo -n ")
	if !contains(dry, "$PP_SRCOPT") || !contains(dry, "security.ubuntu.com") {
		t.Errorf("apt DryRunCommand missing the security-mirror override")
	}

	// Proxmox is apt-based and gets the same treatment (its own repos simply
	// won't match security.ubuntu.com at runtime, so it's a no-op there).
	if !contains(UpdateCommand(MgrAptProxmox, paths, "sudo -n "), "$PP_SRCOPT") {
		t.Errorf("apt_proxmox UpdateCommand should carry the override too")
	}

	// Non-apt managers must be untouched — no stray shell preamble.
	for _, mgr := range []string{MgrDNF, MgrPacman, MgrZypper, MgrApk} {
		if contains(UpdateCommand(mgr, paths, "sudo -n "), "PP_SRCOPT") {
			t.Errorf("%s UpdateCommand should not carry the apt-only workaround", mgr)
		}
	}
}

func contains(s, sub string) bool { return count(s, sub) > 0 }

func count(s, sub string) int {
	n := 0
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			n++
		}
	}
	return n
}

// Every apt invocation that takes a lock waits for it rather than failing on
// the spot — the run of a host whose previous run was aborted mid-upgrade
// otherwise fails with exit 100 until that apt-get finishes on its own.
func TestAptCommandsWaitForTheLock(t *testing.T) {
	paths := map[string]string{"apt-get": "/usr/bin/apt-get", "apt": "/usr/bin/apt"}
	for _, cmd := range []string{UpdateCommand(MgrApt, paths, "sudo -n "), DryRunCommand(MgrApt, paths, "sudo -n ")} {
		for _, verb := range []string{" update", " dist-upgrade"} {
			i := strings.Index(cmd, verb)
			if i < 0 {
				t.Fatalf("expected %q in %q", verb, cmd)
			}
			if !strings.Contains(cmd[:i], "DPkg::Lock::Timeout=600") {
				t.Fatalf("%q runs without the lock timeout: %q", verb, cmd)
			}
		}
	}
	if up := UpdateCommand(MgrApt, paths, "sudo -n "); !strings.Contains(up, "DPkg::Lock::Timeout=600 -y autoremove") {
		t.Fatalf("autoremove runs without the lock timeout: %q", up)
	}
}

// The cache lock is waited for by us, before every apt step, because apt
// itself only waits for dpkg's lock.
func TestAptStepsWaitForTheCacheLockFirst(t *testing.T) {
	paths := map[string]string{"apt-get": "/usr/bin/apt-get", "apt": "/usr/bin/apt"}
	up := UpdateCommand(MgrApt, paths, "sudo -n ")
	if !strings.Contains(up, "pp_wait_apt() {") || !strings.Contains(up, "pp_apt_locked() {") {
		t.Fatalf("update command lacks the wait helper: %q", up)
	}
	for _, step := range []string{"pp_wait_apt && sudo -n /usr/bin/apt-get $PP_SRCOPT", "pp_wait_apt && DEBIAN_FRONTEND=noninteractive", "pp_wait_apt && sudo -n /usr/bin/apt-get -o DPkg::Lock::Timeout=600 -y autoremove"} {
		if !strings.Contains(up, step) {
			t.Fatalf("missing %q in %q", step, up)
		}
	}
	dry := DryRunCommand(MgrApt, paths, "sudo -n ")
	if strings.Count(dry, "pp_wait_apt;") != 2 || !strings.Contains(dry, "pp_wait_apt() {") {
		t.Fatalf("dry-run should wait before update and before the simulation: %q", dry)
	}
	// The probe reads the kernel's lock table; it must not run apt at all.
	if !strings.Contains(up, "/proc/locks") || strings.Contains(up, "Timeout=0 check") {
		t.Fatalf("the lock probe should read /proc/locks, not run apt: %q", up)
	}
}
