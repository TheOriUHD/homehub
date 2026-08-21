// Package sshguard classifies shell commands by how much damage they can do.
//
// It exists for one reason: the assistant can run commands over SSH, and an
// LLM must never be one hallucinated token away from wiping a disk. The
// classifier is deliberately paranoid and fails CLOSED — anything it does not
// positively recognise as read-only requires human confirmation, and a small
// set of catastrophic patterns is refused outright even with confirmation.
//
// It is a safety net, not a sandbox: the real boundary is the SSH user's own
// privileges. Treat it as defence in depth.
package sshguard

import (
	"regexp"
	"strings"
)

// Risk is the verdict for one command.
type Risk string

const (
	// RiskSafe: a read-only command from the allowlist. May run unattended.
	RiskSafe Risk = "safe"
	// RiskConfirm: mutates state, or isn't recognised. Needs the operator's
	// explicit approval before it runs.
	RiskConfirm Risk = "confirm"
	// RiskBlocked: catastrophic and irreversible. Never run, not even with
	// confirmation — the operator can still do it in a real terminal.
	RiskBlocked Risk = "blocked"
)

// Verdict carries the risk plus a human-readable reason.
type Verdict struct {
	Risk    Risk
	Reason  string
	Segment string // the offending segment, for blocked/confirm
}

// readOnly is the allowlist of commands that cannot change the system. Only
// these run without confirmation. Keep it conservative: a command belongs
// here only if EVERY invocation of it is harmless.
var readOnly = map[string]bool{
	"ls": true, "cat": true, "head": true, "tail": true, "less": true, "more": true,
	"grep": true, "egrep": true, "fgrep": true, "rg": true, "awk": true, "sed": false,
	"wc": true, "sort": true, "uniq": true, "cut": true, "tr": true, "column": true,
	"echo": true, "printf": true, "date": true, "uptime": true, "whoami": true, "id": true,
	"hostname": true, "uname": true, "pwd": true, "env": true, "printenv": true,
	"ps": true, "top": true, "htop": true, "free": true, "vmstat": true, "iostat": true,
	"df": true, "du": true, "lsblk": true, "blkid": true, "mount": true, "findmnt": true,
	"lscpu": true, "lsusb": true, "lspci": true, "lsmod": true, "dmesg": true,
	"ip": true, "ifconfig": true, "ss": true, "netstat": true, "route": true,
	"ping": true, "traceroute": true, "dig": true, "nslookup": true, "host": true,
	"which": true, "whereis": true, "type": true, "file": true, "stat": true, "realpath": true,
	"find": true, "locate": true, "tree": true, "readlink": true, "basename": true, "dirname": true,
	"journalctl": true, "last": true, "w": true, "who": true, "users": true, "groups": true,
	"sensors": true, "nproc": true, "arch": true, "getent": true, "sha256sum": true, "md5sum": true,
	"true": true, "false": true, "sleep": true, "seq": true, "yes": false,
}

// subcommandSafe covers commands whose safety depends on the verb that
// follows: `systemctl status` reads, `systemctl restart` acts.
var subcommandSafe = map[string]map[string]bool{
	"systemctl": {"status": true, "list-units": true, "list-unit-files": true, "is-active": true,
		"is-enabled": true, "is-failed": true, "show": true, "cat": true, "get-default": true},
	"docker":  {"ps": true, "images": true, "logs": true, "inspect": true, "stats": true, "version": true, "info": true, "top": true},
	"git":     {"status": true, "log": true, "diff": true, "show": true, "branch": true, "remote": true},
	"apt":     {"list": true, "show": true, "search": true, "policy": true},
	"apt-get": {},
	"dpkg":    {"-l": true, "--list": true, "-s": true, "--status": true, "-L": true},
	"zfs":     {"list": true, "get": true},
	"zpool":   {"status": true, "list": true, "iostat": true},
	"pvesh":   {"get": true},
	"qm":      {"list": true, "status": true, "config": true},
	"pct":     {"list": true, "status": true, "config": true},
	"kubectl": {"get": true, "describe": true, "logs": true, "top": true, "version": true},
}

// catastrophic patterns are refused outright. Each entry is (regexp, reason).
var catastrophic = []struct {
	re     *regexp.Regexp
	reason string
}{
	{regexp.MustCompile(`\brm\b[^|;&]*\s-[a-zA-Z]*[rR][a-zA-Z]*f|\brm\b[^|;&]*\s-[a-zA-Z]*f[a-zA-Z]*[rR]`), "recursive force-delete"},
	{regexp.MustCompile(`\bmkfs(\.[a-z0-9]+)?\b`), "formats a filesystem"},
	{regexp.MustCompile(`\bdd\b[^|;&]*\bof=/dev/`), "writes raw to a block device"},
	{regexp.MustCompile(`\b(shred|wipefs)\b[^|;&]*(/dev/|--all|-a)`), "destroys device data"},
	{regexp.MustCompile(`>\s*/dev/[sn][dv][a-z]`), "overwrites a block device"},
	{regexp.MustCompile(`:\s*\(\s*\)\s*\{.*\|.*&.*\}\s*;?\s*:`), "fork bomb"},
	{regexp.MustCompile(`\bparted\b[^|;&]*\b(mklabel|rm)\b|\bsgdisk\b[^|;&]*(-Z|--zap)`), "repartitions a disk"},
	{regexp.MustCompile(`\bchmod\b[^|;&]*\s-[a-zA-Z]*R[a-zA-Z]*\s+(777|000)\s+/\s*$`), "wrecks permissions on /"},
	{regexp.MustCompile(`\b(userdel|groupdel)\b[^|;&]*\broot\b`), "deletes the root account"},
	{regexp.MustCompile(`>\s*/etc/(passwd|shadow|fstab)\b`), "overwrites a critical system file"},
	{regexp.MustCompile(`\bcryptsetup\b[^|;&]*\b(erase|luksErase|luksFormat)\b`), "destroys an encrypted volume"},
	{regexp.MustCompile(`\bzpool\b[^|;&]*\bdestroy\b|\bzfs\b[^|;&]*\bdestroy\b[^|;&]*-[rR]`), "destroys a ZFS pool/dataset tree"},
	{regexp.MustCompile(`\bhistory\s+-c\b|\b>\s*~?/?\.bash_history\b`), "erases shell history (audit evasion)"},
	{regexp.MustCompile(`\biptables\b[^|;&]*\s-F\b|\bufw\b[^|;&]*\bdisable\b`), "tears down the firewall"},
}

// rmRootRe catches `rm` aimed at the filesystem root or a bare glob of it.
var rmRootRe = regexp.MustCompile(`\brm\b[^|;&]*\s(/|/\*)(\s|$)`)

// Classify returns the strictest verdict across every segment of the command
// (split on ; && || | and newlines), so a safe prefix can never smuggle a
// dangerous suffix past the check.
func Classify(command string) Verdict {
	cmd := strings.TrimSpace(command)
	if cmd == "" {
		return Verdict{Risk: RiskConfirm, Reason: "empty command"}
	}
	// Catastrophic checks run against the WHOLE string first: some patterns
	// (fork bombs, redirects) straddle segment boundaries.
	for _, c := range catastrophic {
		if c.re.MatchString(cmd) {
			return Verdict{Risk: RiskBlocked, Reason: c.reason, Segment: cmd}
		}
	}
	if rmRootRe.MatchString(cmd) {
		return Verdict{Risk: RiskBlocked, Reason: "deletes the filesystem root", Segment: cmd}
	}

	worst := Verdict{Risk: RiskSafe, Reason: "read-only"}
	for _, seg := range splitSegments(cmd) {
		v := classifySegment(seg)
		if rank(v.Risk) > rank(worst.Risk) {
			worst = v
		}
	}
	return worst
}

func rank(r Risk) int {
	switch r {
	case RiskBlocked:
		return 3
	case RiskConfirm:
		return 2
	default:
		return 1
	}
}

// splitSegments breaks a command line on shell separators.
var sepRe = regexp.MustCompile(`\|\||&&|[;|\n]`)

func splitSegments(cmd string) []string {
	parts := sepRe.Split(cmd, -1)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func classifySegment(seg string) Verdict {
	// Any write redirection mutates the target.
	if strings.Contains(seg, ">") {
		return Verdict{Risk: RiskConfirm, Reason: "writes to a file", Segment: seg}
	}
	fields := strings.Fields(seg)
	if len(fields) == 0 {
		return Verdict{Risk: RiskConfirm, Reason: "unparseable", Segment: seg}
	}
	// Strip env-var assignments and a leading sudo; sudo itself only raises
	// the stakes, so a sudo'd read stays safe but is reported as such.
	i := 0
	for i < len(fields) && strings.Contains(fields[i], "=") && !strings.HasPrefix(fields[i], "-") {
		i++
	}
	if i < len(fields) && (fields[i] == "sudo" || fields[i] == "doas") {
		i++
		for i < len(fields) && strings.HasPrefix(fields[i], "-") {
			i++
		}
	}
	if i >= len(fields) {
		return Verdict{Risk: RiskConfirm, Reason: "unparseable", Segment: seg}
	}
	base := fields[i]
	if slash := strings.LastIndex(base, "/"); slash >= 0 {
		base = base[slash+1:]
	}

	if verbs, ok := subcommandSafe[base]; ok {
		for _, f := range fields[i+1:] {
			if strings.HasPrefix(f, "-") && !verbs[f] {
				continue
			}
			if verbs[f] {
				return Verdict{Risk: RiskSafe, Reason: "read-only", Segment: seg}
			}
			break
		}
		return Verdict{Risk: RiskConfirm, Reason: base + " can change system state", Segment: seg}
	}
	if readOnly[base] {
		return Verdict{Risk: RiskSafe, Reason: "read-only", Segment: seg}
	}
	return Verdict{Risk: RiskConfirm, Reason: "not a known read-only command", Segment: seg}
}
