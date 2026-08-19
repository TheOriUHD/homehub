package patchpilot

import (
	"context"
	"regexp"
	"strings"
)

// detectionProbe is a single shell round-trip that emits ---SECTION---
// delimited output ParseHostFacts splits back into HostFacts. One login, one
// command — cheap and idempotent.
// A minimal non-interactive SSH session can hand us a PATH that omits
// /usr/bin (common on LXC/container images), so `command -v` alone misses
// tools that are plainly installed. Set a sane PATH first, and resolve the
// package manager by BOTH command -v and a direct directory search — the
// same belt-and-suspenders the bootstrap resolver uses, so detection can
// never be stricter than the install that follows.
const detectionProbe = `export PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:$PATH
have() { command -v "$1" >/dev/null 2>&1 && return 0; for d in /usr/bin /bin /usr/sbin /sbin /usr/local/bin /usr/local/sbin; do [ -x "$d/$1" ] && return 0; done; return 1; }
echo '---OS---'; cat /etc/os-release 2>/dev/null
echo '---ARCH---'; uname -m
echo '---HOSTNAME---'; hostname 2>/dev/null || cat /etc/hostname 2>/dev/null
echo '---PKGMGR---'; for m in apt-get dnf pacman zypper apk; do have "$m" && echo "$m"; done
echo '---PROXMOX---'; [ -f /etc/pve/.version ] && echo yes
echo '---HAOS---'; grep -qi "home assistant" /etc/os-release 2>/dev/null && echo yes
echo '---TRUENAS---'; grep -qi truenas /etc/version 2>/dev/null && echo yes
echo '---SUDO---'; have sudo && echo yes
echo '---SUDO_NPW---'; sudo -n true 2>/dev/null && echo yes
echo '---SUDOERS_FILE---'; [ -f /etc/sudoers ] && echo yes
echo '---SUDOERS_INCLUDE---'; grep -qE '^[@#]includedir[[:space:]]+/etc/sudoers.d' /etc/sudoers 2>/dev/null && echo yes
echo '---SU---'; have su && echo yes
echo '---UID---'; id -u
echo '---PPUSER---'; id patchpilot >/dev/null 2>&1 && echo yes
echo '---END---'
`

// HostFacts is what detection learns about a target in one probe.
type HostFacts struct {
	OSPretty             string
	Arch                 string
	Hostname             string
	PackageManager       string
	IsProxmox            bool
	IsHAOS               bool
	IsTrueNAS            bool
	HasSudo              bool
	SudoNoPasswd         bool
	SudoersFileExists    bool
	SudoersIncludeDir    bool
	HasSu                bool
	UID                  int
	PatchpilotUserExists bool
}

var prettyRe = regexp.MustCompile(`PRETTY_NAME="?([^"\n]+)"?`)

// Probe runs the detection probe and returns BOTH the raw output and the
// parsed facts — the raw output is invaluable when a host reports something
// unexpected (odd shell, restricted PATH, login banner).
func Probe(ctx context.Context, conn *SSHConn) (string, *HostFacts, error) {
	// Run under /bin/sh over stdin so a non-POSIX login shell (csh/tcsh/fish)
	// can't mangle the sh-syntax probe.
	out, _, err := conn.RunScript(ctx, detectionProbe)
	if err != nil {
		return "", nil, err
	}
	return out, ParseHostFacts(out), nil
}

// Detect runs the probe over an onboarding connection and parses the facts.
func Detect(ctx context.Context, conn *SSHConn) (*HostFacts, error) {
	_, facts, err := Probe(ctx, conn)
	return facts, err
}

// ParseHostFacts splits the delimited probe output into structured facts.
func ParseHostFacts(out string) *HostFacts {
	sections := map[string]string{}
	var cur string
	var buf []string
	flush := func() {
		if cur != "" {
			sections[cur] = strings.TrimSpace(strings.Join(buf, "\n"))
		}
		buf = nil
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "---") && strings.HasSuffix(strings.TrimSpace(line), "---") {
			flush()
			cur = strings.Trim(strings.TrimSpace(line), "-")
			continue
		}
		buf = append(buf, line)
	}
	flush()

	f := &HostFacts{
		Arch:                 sections["ARCH"],
		Hostname:             firstLine(sections["HOSTNAME"]),
		IsProxmox:            sections["PROXMOX"] == "yes",
		IsHAOS:               sections["HAOS"] == "yes",
		IsTrueNAS:            sections["TRUENAS"] == "yes",
		HasSudo:              sections["SUDO"] == "yes",
		SudoNoPasswd:         sections["SUDO_NPW"] == "yes",
		SudoersFileExists:    sections["SUDOERS_FILE"] == "yes",
		SudoersIncludeDir:    sections["SUDOERS_INCLUDE"] == "yes",
		HasSu:                sections["SU"] == "yes",
		PatchpilotUserExists: sections["PPUSER"] == "yes",
	}
	if m := prettyRe.FindStringSubmatch(sections["OS"]); m != nil {
		f.OSPretty = m[1]
	}
	if uid := firstLine(sections["UID"]); uid != "" {
		if uid == "0" {
			f.UID = 0
		} else {
			f.UID = -1 // non-root; exact value not needed
		}
	}
	// First package manager found wins, in probe order.
	for _, m := range strings.Split(sections["PKGMGR"], "\n") {
		switch strings.TrimSpace(m) {
		case "apt-get":
			f.PackageManager = MgrApt
		case "dnf":
			f.PackageManager = MgrDNF
		case "pacman":
			f.PackageManager = MgrPacman
		case "zypper":
			f.PackageManager = MgrZypper
		case "apk":
			f.PackageManager = MgrApk
		}
		if f.PackageManager != "" {
			break
		}
	}
	// apt on a Proxmox host is its own manager (pinned/held kernels).
	if f.PackageManager == MgrApt && f.IsProxmox {
		f.PackageManager = MgrAptProxmox
	}
	return f
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

// ChooseEscalation picks the escalation mode from the facts and whether a
// root password was supplied. Priority per §7.2: direct-root, sudo-nopasswd,
// su-root (only with a root password), sudo-password.
func ChooseEscalation(f *HostFacts, haveLoginPassword, haveRootPassword bool) (EscalationMode, string) {
	switch {
	case f.UID == 0:
		return EscDirectRoot, ""
	case f.SudoNoPasswd:
		return EscSudoNoPasswd, ""
	case f.HasSudo && haveLoginPassword:
		return EscSudoPassword, ""
	case f.HasSu && haveRootPassword:
		return EscSuRoot, ""
	}
	// Nothing usable — describe the gap.
	if !f.HasSudo && !f.HasSu {
		return "", "neither sudo nor su is available and you are not root"
	}
	return "", "sudo needs a password (or root's password for su) to elevate this account"
}

// StandardRefusal returns a message when the host is really an API-managed
// appliance and should not be onboarded as a Standard SSH host.
func StandardRefusal(f *HostFacts) string {
	if f.IsHAOS {
		return "this looks like Home Assistant OS — manage it with the Home Assistant integration, not PatchPilot"
	}
	if f.IsTrueNAS {
		return "this looks like TrueNAS — manage it with the TrueNAS integration, not PatchPilot"
	}
	if f.PackageManager == "" {
		if f.OSPretty != "" {
			return "no supported package manager (apt/dnf/pacman/zypper/apk) was found on " + f.OSPretty
		}
		return "connected, but no supported package manager (apt/dnf/pacman/zypper/apk) was found — is this a Linux host with SSH?"
	}
	return ""
}
