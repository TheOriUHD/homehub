package patchpilot

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Package-manager matrix. Harvested from the patchpilot spec §8. Every
// command runs non-interactively so a missing NOPASSWD entry fails loudly
// instead of hanging on a password prompt. Paths is the resolved
// absolute-path map from detection (§7.3): the remote account is whitelisted
// by absolute path, so we invoke by absolute path too.

const (
	MgrApt        = "apt"
	MgrAptProxmox = "apt_proxmox"
	MgrDNF        = "dnf"
	MgrPacman     = "pacman"
	MgrZypper     = "zypper"
	MgrApk        = "apk"
)

// AllManagers is the detection preference order (first found wins).
var AllManagers = []string{MgrApt, MgrDNF, MgrPacman, MgrZypper, MgrApk}

// probeBinaries are the command-line names detection looks for, per manager.
func managerBinaries(mgr string) []string {
	switch mgr {
	case MgrApt, MgrAptProxmox:
		return []string{"apt-get", "apt", "dpkg"}
	case MgrDNF:
		return []string{"dnf", "needs-restarting"}
	case MgrPacman:
		return []string{"pacman"}
	case MgrZypper:
		return []string{"zypper"}
	case MgrApk:
		return []string{"apk"}
	}
	return nil
}

// sudoersBinaries are the package-manager binaries actually invoked via sudo,
// i.e. the only ones that belong in the NOPASSWD drop-in. Deliberately narrower
// than managerBinaries (which also lists detection-only names): apt and dpkg
// are never run with sudo, and granting them NOPASSWD would hand out a needless
// second direct arbitrary-root primitive (dpkg maintainer scripts) beyond the
// one apt-get already implies. Debian therefore lists apt-get alone.
func sudoersBinaries(mgr string) []string {
	switch mgr {
	case MgrApt, MgrAptProxmox:
		return []string{"apt-get"}
	case MgrDNF:
		return []string{"dnf"} // needs-restarting is added separately below
	case MgrPacman:
		return []string{"pacman"}
	case MgrZypper:
		return []string{"zypper"}
	case MgrApk:
		return []string{"apk"}
	}
	return nil
}

// RequiredCommands must all resolve to an absolute path or bootstrap aborts.
func RequiredCommands(mgr string) []string {
	base := append([]string{}, managerBinaries(mgr)...)
	base = append(base, "test", "systemctl", "reboot", "shutdown")
	return base
}

// ManagerDisplayName is a friendly label for the UI.
func ManagerDisplayName(mgr string) string {
	switch mgr {
	case MgrApt:
		return "APT"
	case MgrAptProxmox:
		return "APT (Proxmox)"
	case MgrDNF:
		return "DNF"
	case MgrPacman:
		return "pacman"
	case MgrZypper:
		return "zypper"
	case MgrApk:
		return "apk"
	}
	return mgr
}

// path returns the resolved absolute path for a binary, falling back to the
// bare name (so commands still form on a host whose resolution was partial).
func path(paths map[string]string, name string) string {
	if p := paths[name]; p != "" {
		return p
	}
	return name
}

// aptAcquire bounds apt's network fetches so one slow or blackholed mirror
// (the classic security.ubuntu.com-over-IPv6 stall) can't hang a run: cap each
// connection at 20s, retry a bounded 3 times, and prefer IPv4 to sidestep a
// dead AAAA route. Homelab hosts are effectively always IPv4-reachable; a
// genuinely IPv6-only host is the rare exception.
const aptAcquire = " -o Acquire::Retries=3 -o Acquire::http::Timeout=20 -o Acquire::https::Timeout=20 -o Acquire::ForceIPv4=true"

// UpdateCommand builds the full upgrade command, wrapped by the given sudo
// prefix (e.g. "sudo -n " or "" for a root session).
func UpdateCommand(mgr string, paths map[string]string, sudo string) string {
	aptGet := path(paths, "apt-get")
	switch mgr {
	case MgrApt, MgrAptProxmox:
		return fmt.Sprintf(
			"%s%s%s update && DEBIAN_FRONTEND=noninteractive %s%s%s -y -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold dist-upgrade && %s%s -y autoremove",
			sudo, aptGet, aptAcquire, sudo, aptGet, aptAcquire, sudo, aptGet)
	case MgrDNF:
		return sudo + path(paths, "dnf") + " -y upgrade"
	case MgrPacman:
		return sudo + path(paths, "pacman") + " -Syu --noconfirm"
	case MgrZypper:
		return sudo + path(paths, "zypper") + " -n update"
	case MgrApk:
		apk := path(paths, "apk")
		return fmt.Sprintf("%s%s update && %s%s upgrade", sudo, apk, sudo, apk)
	}
	return ""
}

// DryRunCommand simulates an upgrade without applying it.
func DryRunCommand(mgr string, paths map[string]string, sudo string) string {
	switch mgr {
	case MgrApt, MgrAptProxmox:
		aptGet := path(paths, "apt-get")
		apt := path(paths, "apt")
		return fmt.Sprintf("%s%s"+aptAcquire+" update >/dev/null 2>&1; %s%s -s dist-upgrade; %s%s list --upgradable 2>/dev/null",
			sudo, aptGet, sudo, aptGet, sudo, apt)
	case MgrDNF:
		// check-update exits 100 when updates exist — not an error.
		return sudo + path(paths, "dnf") + " check-update; true"
	case MgrPacman:
		return sudo + path(paths, "pacman") + " -Sup 2>/dev/null; true"
	case MgrZypper:
		return sudo + path(paths, "zypper") + " -n list-updates"
	case MgrApk:
		apk := path(paths, "apk")
		return fmt.Sprintf("%s%s update >/dev/null 2>&1; %s%s -s upgrade", sudo, apk, sudo, apk)
	}
	return ""
}

// RebootRequiredCommand exits 0 when the host needs a reboot.
func RebootRequiredCommand(mgr string, paths map[string]string, sudo string) string {
	test := path(paths, "test")
	switch mgr {
	case MgrApt, MgrAptProxmox:
		return fmt.Sprintf("%s -f /var/run/reboot-required", test)
	case MgrDNF:
		return sudo + path(paths, "needs-restarting") + " -r >/dev/null 2>&1; test $? -eq 1"
	case MgrPacman:
		return `test "$(uname -r | sed 's/-[a-z]*$//')" != "$(pacman -Q linux 2>/dev/null | awk '{print $2}' | sed 's/\\.arch/.arch/')" `
	case MgrZypper:
		return `test "$(uname -r)" != "$(rpm -q --qf '%{VERSION}-%{RELEASE}' kernel-default 2>/dev/null)"`
	case MgrApk:
		return fmt.Sprintf("%s -f /var/run/reboot-required", test)
	}
	return "false"
}

// ---- sudoers ----------------------------------------------------------------

// SudoersContent renders the least-privilege drop-in for /etc/sudoers.d.
// Absolute paths only, no wildcards; reboot verbs are argument-constrained.
// Sorted for determinism (so repair-sudoers is idempotent).
func SudoersContent(user, mgr string, paths map[string]string) string {
	cmds := map[string]bool{}
	for _, b := range sudoersBinaries(mgr) {
		if p := paths[b]; strings.HasPrefix(p, "/") {
			cmds[p] = true
		}
	}
	if p := paths["test"]; strings.HasPrefix(p, "/") {
		cmds[p] = true
	}
	if mgr == MgrDNF {
		if p := paths["needs-restarting"]; strings.HasPrefix(p, "/") {
			cmds[p] = true
		}
	}
	// Plain path-only entries.
	var list []string
	for c := range cmds {
		list = append(list, c)
	}
	sort.Strings(list)
	// Argument-constrained reboot verbs.
	systemctl := path(paths, "systemctl")
	shutdown := path(paths, "shutdown")
	reboot := path(paths, "reboot")
	if strings.HasPrefix(reboot, "/") {
		list = append(list, reboot)
	}
	if strings.HasPrefix(systemctl, "/") {
		list = append(list, systemctl+" reboot")
	}
	if strings.HasPrefix(shutdown, "/") {
		list = append(list, shutdown+" -r now")
	}
	return fmt.Sprintf("%s ALL=(ALL) NOPASSWD: %s\n", user, strings.Join(list, ", "))
}

// RebootCommand issues an explicit reboot (never called automatically).
func RebootCommand(paths map[string]string, sudo string) string {
	return sudo + path(paths, "systemctl") + " reboot"
}

// InstallSudoCommand installs the sudo package — used only when onboarding a
// root host that doesn't have it, so the managed account can be granted
// NOPASSWD access. Runs as root (no sudo prefix).
func InstallSudoCommand(mgr string) string {
	switch mgr {
	case MgrApt, MgrAptProxmox:
		return "apt-get update >/dev/null 2>&1; DEBIAN_FRONTEND=noninteractive apt-get install -y sudo"
	case MgrDNF:
		return "dnf install -y sudo"
	case MgrPacman:
		return "pacman -Sy --noconfirm sudo"
	case MgrZypper:
		return "zypper -n install sudo"
	case MgrApk:
		return "apk add sudo"
	}
	return ""
}

// ---- dry-run parsing --------------------------------------------------------

// DryRunResult is the parsed outcome of a simulate command. Counts are
// heuristic and locale-sensitive; RawOutput is authoritative.
type DryRunResult struct {
	PackagesUpgraded int    `json:"packagesUpgraded"`
	PackagesRemoved  int    `json:"packagesRemoved"`
	RebootRequired   bool   `json:"rebootRequired"`
	Summary          string `json:"summary"`
	RawOutput        string `json:"rawOutput"`
}

var (
	aptInst    = regexp.MustCompile(`(?m)^Inst `)
	aptRemv    = regexp.MustCompile(`(?m)^Remv `)
	apkChange  = regexp.MustCompile(`(?m)^\s*\(\d+/\d+\)\s+(Upgrading|Installing)`)
	pacmanLine = regexp.MustCompile(`(?m)^\S+/\S+`)
)

// ParseDryRun turns simulate output into counts.
func ParseDryRun(mgr, output string) DryRunResult {
	res := DryRunResult{RawOutput: output}
	switch mgr {
	case MgrApt, MgrAptProxmox:
		res.PackagesUpgraded = len(aptInst.FindAllString(output, -1))
		res.PackagesRemoved = len(aptRemv.FindAllString(output, -1))
	case MgrDNF:
		// Lines after "Last metadata" that look like "name.arch  ver  repo".
		res.PackagesUpgraded = countDNF(output)
	case MgrPacman:
		res.PackagesUpgraded = len(pacmanLine.FindAllString(output, -1))
	case MgrZypper:
		res.PackagesUpgraded = strings.Count(output, "\nv | ") + strings.Count(output, "The following ")
		res.PackagesUpgraded = countZypper(output)
	case MgrApk:
		res.PackagesUpgraded = len(apkChange.FindAllString(output, -1))
	}
	if res.PackagesUpgraded > 0 {
		res.Summary = fmt.Sprintf("%d update(s) available", res.PackagesUpgraded)
	} else {
		res.Summary = "up to date"
	}
	return res
}

func countDNF(output string) int {
	n := 0
	for _, line := range strings.Split(output, "\n") {
		f := strings.Fields(line)
		if len(f) >= 3 && strings.Contains(f[0], ".") && !strings.HasPrefix(line, " ") &&
			!strings.HasPrefix(line, "Last") && !strings.HasPrefix(line, "Obsoleting") {
			// crude: name.arch version repo
			if strings.ContainsAny(f[1], "0123456789") {
				n++
			}
		}
	}
	return n
}

func countZypper(output string) int {
	n := 0
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "v | ") { // zypper table rows for updates
			n++
		}
	}
	return n
}
