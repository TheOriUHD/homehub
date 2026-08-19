package patchpilot

import "testing"

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
