// Package sshguard classifies shell commands by how much damage they can do.
//
// It exists for one reason: the assistant can run commands over SSH, and an
// LLM must never be one hallucinated token away from wiping a disk. The
// classifier is deliberately paranoid and fails CLOSED — anything it does not
// positively recognise as read-only requires human confirmation, and a small
// set of catastrophic patterns is refused outright even with confirmation.
//
// "Recognise" means more than the program name. A reader like find or sort
// has flags that delete or write files, a shell runs whatever a $(…) or a
// redirect smuggles in, and a wrapper like env or xargs runs a second program
// the allowlist never saw. So the command is lexed the way the shell would
// split it, every simple command in it is judged on its own, and a program
// whose flags can act is safe only with the flags that cannot.
//
// It is a safety net, not a sandbox: the real boundary is the SSH user's own
// privileges. Treat it as defence in depth.
package sshguard

import (
	"path"
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

// readOnly is the allowlist of commands that cannot change the system with
// ANY arguments. Only these run without a look at their flags. A program with
// even one acting flag (sort -o, find -delete, date -s) belongs in argChecks
// instead, and one that runs other programs (awk, env, xargs) belongs nowhere.
var readOnly = map[string]bool{
	"ls": true, "cat": true, "head": true, "tail": true,
	"grep": true, "egrep": true, "fgrep": true,
	"wc": true, "cut": true, "tr": true, "column": true,
	"echo": true, "printf": true, "uptime": true, "whoami": true, "id": true,
	"uname": true, "pwd": true, "printenv": true,
	"ps": true, "top": true, "htop": true, "free": true, "vmstat": true, "iostat": true,
	"df": true, "du": true, "lsblk": true, "findmnt": true,
	"lscpu": true, "lsusb": true, "lspci": true, "lsmod": true,
	"netstat": true, "ping": true, "traceroute": true, "dig": true, "nslookup": true, "host": true,
	"which": true, "whereis": true, "type": true, "stat": true, "realpath": true,
	"locate": true, "readlink": true, "basename": true, "dirname": true,
	"last": true, "w": true, "who": true, "users": true, "groups": true,
	"nproc": true, "arch": true, "getent": true, "sha256sum": true, "md5sum": true,
	"true": true, "false": true, "sleep": true, "seq": true,
}

// argChecks covers readers that also have a flag or two that act. Each
// returns "" when the arguments only read, or the reason they do not.
var argChecks = map[string]func(args []string) string{
	"find": func(args []string) string {
		for _, a := range args {
			switch a {
			case "-delete", "-exec", "-execdir", "-ok", "-okdir":
				return "find " + a + " acts on what it finds"
			case "-fprint", "-fprint0", "-fprintf", "-fls":
				return "find " + a + " writes a file"
			}
		}
		return ""
	},
	"sort": func(args []string) string {
		for _, a := range args {
			if a == "--output" || strings.HasPrefix(a, "--output=") || shortFlagHas(a, "o") {
				return "sort -o writes a file"
			}
			if strings.HasPrefix(a, "--compress-program") {
				return "sort --compress-program runs a program"
			}
		}
		return ""
	},
	// env with a command runs that command; on its own it only prints.
	"env": func(args []string) string {
		for _, a := range args {
			if a != "-0" && a != "--null" {
				return "env runs another program"
			}
		}
		return ""
	},
	"ip":       checkIP,
	"ifconfig": checkIfconfig,
	"route": func(args []string) string {
		for _, a := range args {
			if !strings.HasPrefix(a, "-") {
				return "route " + a + " changes the routing table"
			}
		}
		return ""
	},
	"mount": func(args []string) string {
		for _, a := range args {
			if a != "-l" {
				return "mount with arguments mounts something"
			}
		}
		return ""
	},
	"hostname": func(args []string) string {
		for _, a := range args {
			if !hostnameReadFlags[a] {
				return "hostname " + a + " can rename the machine"
			}
		}
		return ""
	},
	"date":    checkDate,
	"tree":    rejectFlags("tree -o writes a file", "-o"),
	"uniq":    checkUniq,
	"less":    rejectPlus,
	"more":    rejectPlus,
	"rg":      rejectFlags("rg --pre runs a program on every file", "--pre"),
	"blkid":   rejectFlags("blkid can rewrite its cache file", "-c", "-g", "--cache-file", "--garbage-collect"),
	"file":    rejectFlags("file -C writes a compiled magic file", "-C", "--compile"),
	"ss":      rejectFlags("ss can kill sockets or write a dump file", "-K", "--kill", "-D", "--diag"),
	"sensors": rejectFlags("sensors -s writes chip settings", "-s", "--set"),
	"journalctl": rejectFlags("journalctl can rotate, vacuum or rewrite the journal",
		"--vacuum-size", "--vacuum-time", "--vacuum-files", "--rotate", "--flush", "--sync",
		"--relinquish-var", "--smart-relinquish-var", "--setup-keys", "--update-catalog", "--cursor-file"),
	"dmesg": rejectFlags("dmesg can clear the ring buffer or change the console level",
		"-c", "-C", "-n", "-D", "-E", "--clear", "--read-clear", "--console-level", "--console-off", "--console-on"),
}

// shortFlagHas reports whether a single-dash flag cluster like -rno contains
// any of the given letters — sort -ro writes just as sort -o does.
func shortFlagHas(arg, letters string) bool {
	if len(arg) < 2 || arg[0] != '-' || arg[1] == '-' {
		return false
	}
	return strings.ContainsAny(arg[1:], letters)
}

// rejectFlags refuses any of the named flags, in their --flag=value and
// clustered -xyz spellings too.
func rejectFlags(reason string, flags ...string) func([]string) string {
	return func(args []string) string {
		for _, a := range args {
			for _, f := range flags {
				if a == f || strings.HasPrefix(a, f+"=") {
					return reason
				}
				if len(f) == 2 && shortFlagHas(a, f[1:]) {
					return reason
				}
			}
		}
		return ""
	}
}

// rejectPlus refuses less/more start-up commands (+cmd), which include the !
// shell escape.
func rejectPlus(args []string) string {
	for _, a := range args {
		if strings.HasPrefix(a, "+") {
			return "a +command runs at start-up and can reach a shell"
		}
	}
	return ""
}

var hostnameReadFlags = map[string]bool{
	"-a": true, "-A": true, "-d": true, "-f": true, "-i": true, "-I": true, "-s": true,
	"--alias": true, "--all-fqdns": true, "--domain": true, "--fqdn": true, "--long": true,
	"--ip-address": true, "--all-ip-addresses": true, "--short": true,
}

// ipObjects and ipReadVerbs: `ip addr`, `ip -br link show`, `ip route get …`
// read; every other verb (add, del, set, flush, exec …) acts.
var ipObjects = map[string]bool{
	"a": true, "addr": true, "address": true, "l": true, "link": true,
	"r": true, "ro": true, "route": true, "n": true, "neigh": true, "neighbour": true,
	"rule": true, "ru": true, "maddr": true, "mroute": true, "tunnel": true, "tun": true,
	"netns": true,
}

var ipReadVerbs = map[string]bool{
	"show": true, "sh": true, "s": true, "list": true, "ls": true, "lst": true, "l": true, "get": true,
}

func checkIP(args []string) string {
	i := 0
	for i < len(args) && strings.HasPrefix(args[i], "-") {
		switch a := args[i]; {
		case a == "-b", a == "-batch", a == "--batch", a == "-force", a == "--force":
			return "ip " + a + " runs commands from a file"
		case a == "-n", a == "-netns", a == "--netns":
			i++ // takes a namespace name
		}
		i++
	}
	if i >= len(args) {
		return "" // `ip` alone prints usage
	}
	if !ipObjects[args[i]] {
		return "ip " + args[i] + " is not a read-only ip command"
	}
	if i+1 >= len(args) {
		return "" // `ip a`, `ip r`: the default verb is show
	}
	if !ipReadVerbs[args[i+1]] {
		return "ip " + args[i] + " " + args[i+1] + " changes the network"
	}
	return ""
}

// checkIfconfig allows listing (`ifconfig`, `ifconfig -a`, `ifconfig eth0`);
// anything after an interface name (up, down, an address, mtu …) sets it.
func checkIfconfig(args []string) string {
	positional := 0
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			if a != "-a" && a != "-s" && a != "-v" {
				return "ifconfig " + a + " is not a listing flag"
			}
			continue
		}
		positional++
	}
	if positional > 1 {
		return "ifconfig with settings changes an interface"
	}
	return ""
}

// checkDate allows printing dates in any format but refuses setting the clock
// (-s, or a bare MMDDhhmm operand).
func checkDate(args []string) string {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-d", a == "--date", a == "-r", a == "--reference", a == "-f", a == "--file":
			i++ // the value is a date or a path to read, never a setting
		case a == "--set" || strings.HasPrefix(a, "--set="), shortFlagHas(a, "s"):
			return "date -s sets the clock"
		case strings.HasPrefix(a, "-"):
		case strings.HasPrefix(a, "+"):
		default:
			return "date " + a + " sets the clock"
		}
	}
	return ""
}

// checkUniq refuses a second file operand: `uniq in out` overwrites out.
func checkUniq(args []string) string {
	positional := 0
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-f", a == "-s", a == "-w":
			i++
		case strings.HasPrefix(a, "-") && a != "-":
		default:
			positional++
		}
	}
	if positional > 1 {
		return "uniq with two files writes the second"
	}
	return ""
}

// subcommand describes a command whose safety depends on its verb:
// `systemctl status` reads, `systemctl restart` acts.
type subcommand struct {
	verbs map[string]bool
	// global flags allowed before the verb. Anything else there fails
	// closed: a flag that takes a value could make the value look like the
	// verb (`kubectl -n get delete pod x` deletes).
	plain map[string]bool
	// global flags that consume the next word, so it is skipped.
	valued map[string]bool
	// banned flags refused even in --flag=value form.
	banned map[string]bool
	// verbArgs inspects what follows the verb; nil means anything goes.
	verbArgs func(verb string, args []string) string
}

func set(words ...string) map[string]bool {
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}

var subcommands = map[string]subcommand{
	"systemctl": {
		verbs: set("status", "list-units", "list-unit-files", "list-timers", "list-sockets", "is-active",
			"is-enabled", "is-failed", "show", "cat", "get-default"),
		plain: set("--no-pager", "--user", "--system", "--all", "-a", "--full", "-l", "--failed",
			"--plain", "--no-legend", "-q", "--quiet"),
	},
	"docker": {verbs: set("ps", "images", "logs", "inspect", "stats", "version", "info", "top")},
	"git": {
		verbs:  set("status", "log", "diff", "show", "branch", "remote"),
		plain:  set("--no-pager", "-P", "--no-optional-locks"),
		valued: set("-C"),
		// These point git at other programs or other configuration.
		banned:   set("-c", "--config-env", "--exec-path"),
		verbArgs: checkGitVerb,
	},
	"apt":     {verbs: set("list", "show", "search", "policy")},
	"apt-get": {verbs: set()},
	"zfs":     {verbs: set("list", "get")},
	"zpool":   {verbs: set("status", "list", "iostat")},
	"pvesh":   {verbs: set("get")},
	"qm":      {verbs: set("list", "status", "config")},
	"pct":     {verbs: set("list", "status", "config")},
	"kubectl": {
		verbs:  set("get", "describe", "logs", "top", "version"),
		valued: set("-n", "--namespace", "--context"),
	},
}

func checkGitVerb(verb string, args []string) string {
	switch verb {
	case "log", "diff", "show":
		for _, a := range args {
			if a == "--output" || strings.HasPrefix(a, "--output=") {
				return "git --output writes a file"
			}
			if a == "--ext-diff" {
				return "git --ext-diff runs an external program"
			}
		}
	case "branch":
		// Listing only: a bare name creates a branch, -d/-m/-c delete,
		// move and copy. Patterns are names only after --list.
		listing := false
		for _, a := range args {
			if a == "--list" || a == "-l" {
				listing = true
			}
		}
		for _, a := range args {
			switch {
			case gitBranchListFlags[a], strings.HasPrefix(a, "--sort="), strings.HasPrefix(a, "--format="),
				strings.HasPrefix(a, "--color="), strings.HasPrefix(a, "--column="):
			case !strings.HasPrefix(a, "-") && listing:
			default:
				return "git branch " + a + " changes branches"
			}
		}
	case "remote":
		if len(args) == 0 || (len(args) == 1 && (args[0] == "-v" || args[0] == "--verbose")) {
			return ""
		}
		if args[0] == "show" || args[0] == "get-url" {
			return ""
		}
		return "git remote " + args[0] + " changes the remotes"
	}
	return ""
}

var gitBranchListFlags = set("-a", "-r", "-v", "-vv", "-l", "--list", "--all", "--remotes", "--verbose",
	"--show-current", "--color", "--no-color", "--column", "--no-column")

// dpkg's read "verbs" are flags; any other action flag next to them acts.
var dpkgReadFlags = set("-l", "--list", "-s", "--status", "-L", "--listfiles", "-S", "--search", "-p", "--print-avail")

// trustedDirs are where a program named by path may live and still be the
// program its name says. ./ls or /tmp/cat is whatever someone put there.
var trustedDirs = set("/bin/", "/usr/bin/", "/sbin/", "/usr/sbin/", "/usr/local/bin/", "/usr/local/sbin/")

// benignEnv are variables a read may set in front of itself. Others can change
// what the program does (LD_PRELOAD, PAGER, GIT_EXTERNAL_DIFF, LESSOPEN …).
func benignEnv(name string) bool {
	return name == "LANG" || name == "LANGUAGE" || name == "TZ" || name == "COLUMNS" ||
		strings.HasPrefix(name, "LC_")
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

// rmPartRe picks out each rm invocation; the two flag patterns then look for
// recursion and force anywhere in it, however the flags are spelled.
var (
	rmPartRe      = regexp.MustCompile(`\brm\b[^|;&\n]*`)
	rmRecursiveRe = regexp.MustCompile(`\s(-[a-zA-Z]*[rR][a-zA-Z]*|--recursive)\b`)
	rmForceRe     = regexp.MustCompile(`\s(-[a-zA-Z]*f[a-zA-Z]*|--force)\b`)
)

// Classify returns the strictest verdict across every simple command in the
// line (split on ; && || | & and newlines, the way the shell splits them), so
// a safe prefix can never smuggle a dangerous suffix past the check.
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
	// The -rf regexp above needs both letters in one cluster; `rm -r -f` and
	// `rm --recursive --force` are the same command.
	for _, part := range rmPartRe.FindAllString(cmd, -1) {
		if rmRecursiveRe.MatchString(part) && rmForceRe.MatchString(part) {
			return Verdict{Risk: RiskBlocked, Reason: "recursive force-delete", Segment: cmd}
		}
	}

	segs, bad := lex(cmd)
	if bad != nil {
		return *bad
	}
	if len(segs) == 0 {
		return Verdict{Risk: RiskConfirm, Reason: "unparseable", Segment: cmd}
	}
	worst := Verdict{Risk: RiskSafe, Reason: "read-only"}
	for _, seg := range segs {
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

// word is one shell word after quote removal. expands marks a word the shell
// will still rewrite ($VAR, a glob, a brace list, ~): its final text — and so
// whether it is a harmless operand or a -delete — is not knowable here.
type word struct {
	text    string
	expands bool
}

// segment is one simple command: the raw text for the verdict, and its words
// with redirections already taken out.
type segment struct {
	raw   string
	words []word
}

// lex splits a command line into simple commands the way a POSIX shell would,
// closely enough to fail closed: anything it cannot follow — command or
// process substitution, a subshell, a here-document, an unbalanced quote, a
// redirect into a file — comes back as a confirm verdict instead of segments.
func lex(cmd string) ([]segment, *Verdict) {
	confirm := func(reason string) ([]segment, *Verdict) {
		return nil, &Verdict{Risk: RiskConfirm, Reason: reason, Segment: cmd}
	}
	const subst = "runs a command substitution"
	var (
		segs     []segment
		cur      []word
		w        strings.Builder
		inWord   bool
		quoted   bool
		expands  bool
		segStart int
	)
	flushWord := func() {
		if inWord {
			cur = append(cur, word{text: w.String(), expands: expands})
		}
		w.Reset()
		inWord, quoted, expands = false, false, false
	}
	flushSeg := func(end, next int) {
		flushWord()
		if len(cur) > 0 {
			segs = append(segs, segment{raw: strings.TrimSpace(cmd[segStart:end]), words: cur})
		}
		cur = nil
		segStart = next
	}
	n := len(cmd)
	for i := 0; i < n; i++ {
		c := cmd[i]
		next := byte(0)
		if i+1 < n {
			next = cmd[i+1]
		}
		switch c {
		case '\'':
			j := strings.IndexByte(cmd[i+1:], '\'')
			if j < 0 {
				return confirm("unbalanced quote")
			}
			w.WriteString(cmd[i+1 : i+1+j])
			inWord, quoted = true, true
			i += j + 1
		case '"':
			inWord, quoted = true, true
			i++
			for ; i < n && cmd[i] != '"'; i++ {
				switch cmd[i] {
				case '\\':
					if i+1 < n {
						i++
						w.WriteByte(cmd[i])
					}
				case '`':
					return confirm(subst)
				case '$':
					if i+1 < n && cmd[i+1] == '(' {
						return confirm(subst)
					}
					expands = true
					w.WriteByte('$')
				default:
					w.WriteByte(cmd[i])
				}
			}
			if i >= n {
				return confirm("unbalanced quote")
			}
		case '\\':
			if i+1 < n {
				i++
				if cmd[i] != '\n' {
					w.WriteByte(cmd[i])
					inWord, quoted = true, true
				}
			}
		case '`':
			return confirm(subst)
		case '$':
			if next == '(' {
				return confirm(subst)
			}
			w.WriteByte(c)
			inWord, expands = true, true
		case '(', ')':
			return confirm("runs a subshell")
		case ' ', '\t', '\r':
			flushWord()
		case '\n', ';':
			flushSeg(i, i+1)
		case '|':
			if next == '|' || next == '&' {
				flushSeg(i, i+2)
				i++
			} else {
				flushSeg(i, i+1)
			}
		case '&':
			switch next {
			case '&':
				flushSeg(i, i+2)
				i++
			case '>':
				flushWord()
				end, reason := redirect(cmd, i+1, true)
				if reason != "" {
					return confirm(reason)
				}
				i = end - 1
			default:
				flushSeg(i, i+1) // a background job is still a command
			}
		case '>', '<':
			if next == '(' {
				return confirm("runs a process substitution")
			}
			// A bare number glued to the operator is a file descriptor
			// (the 2 in 2>/dev/null), not a word of the command.
			if inWord && !quoted && !expands && isDigits(w.String()) {
				w.Reset()
				inWord = false
			}
			flushWord()
			end, reason := redirect(cmd, i, false)
			if reason != "" {
				return confirm(reason)
			}
			i = end - 1
		case '*', '?', '[', '{', '~':
			w.WriteByte(c)
			inWord, expands = true, true
		default:
			w.WriteByte(c)
			inWord = true
		}
	}
	flushSeg(n, n)
	return segs, nil
}

// redirect reads one redirection starting at the operator at i (after the &
// of &>, when amp is set) and returns where it ends. Reading from a file and
// duplicating or closing descriptors are harmless; output may only go to
// /dev/null — anything else writes a file, and a here-document hides a body
// the lexer would have to follow.
func redirect(cmd string, i int, amp bool) (int, string) {
	n := len(cmd)
	op := cmd[i]
	out, dup := amp || op == '>', false
	j := i + 1
	if op == '>' {
		if j < n && (cmd[j] == '>' || cmd[j] == '|') {
			j++
		}
		if j < n && cmd[j] == '&' && !amp {
			dup = true
			j++
		}
	} else {
		if j < n && cmd[j] == '<' {
			return 0, "uses a here-document"
		}
		if j < n && cmd[j] == '&' {
			dup = true
			j++
		}
		if j < n && cmd[j] == '>' {
			out = true // <> opens the file for writing too
			j++
		}
	}
	for j < n && (cmd[j] == ' ' || cmd[j] == '\t') {
		j++
	}
	start := j
	for j < n && !strings.ContainsRune(" \t\r\n;|&<>()", rune(cmd[j])) {
		j++
	}
	target := cmd[start:j]
	switch {
	case target == "":
		return 0, "unparseable redirection"
	case dup && (target == "-" || isDigits(target)):
		return j, ""
	case out && target != "/dev/null":
		return 0, "writes to a file"
	}
	return j, ""
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// isAssignment recognises NAME=value and returns NAME.
func isAssignment(s string) (string, bool) {
	eq := strings.IndexByte(s, '=')
	if eq <= 0 {
		return "", false
	}
	for k, r := range s[:eq] {
		if !(r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || k > 0 && r >= '0' && r <= '9') {
			return "", false
		}
	}
	return s[:eq], true
}

func classifySegment(seg segment) Verdict {
	confirm := func(reason string) Verdict {
		return Verdict{Risk: RiskConfirm, Reason: reason, Segment: seg.raw}
	}
	safe := Verdict{Risk: RiskSafe, Reason: "read-only", Segment: seg.raw}
	fields := seg.words
	i := 0
	// Leading assignments become the program's environment, and plenty of
	// variables change what a program does.
	for i < len(fields) {
		name, ok := isAssignment(fields[i].text)
		if !ok {
			break
		}
		if !benignEnv(name) || fields[i].expands {
			return confirm("sets " + name + ", which can change what the command does")
		}
		i++
	}
	// A leading sudo only raises the stakes, so a sudo'd read stays safe —
	// but only plain sudo: its other options run shells or edit files.
	if i < len(fields) && (fields[i].text == "sudo" || fields[i].text == "doas") {
		i++
		for i < len(fields) && strings.HasPrefix(fields[i].text, "-") {
			switch fields[i].text {
			case "-n", "--non-interactive":
			case "-u", "-g":
				i++
			default:
				return confirm("sudo " + fields[i].text + " is not a plain privilege raise")
			}
			i++
		}
	}
	if i >= len(fields) {
		return confirm("unparseable")
	}
	name := fields[i]
	if name.expands {
		return confirm("the program name is expanded by the shell")
	}
	base := name.text
	if strings.Contains(base, "/") {
		dir, file := path.Split(base)
		if !trustedDirs[dir] {
			return confirm("runs a program outside the system directories")
		}
		base = file
	}
	args := make([]string, 0, len(fields)-i-1)
	argsExpand := false
	for _, f := range fields[i+1:] {
		args = append(args, f.text)
		argsExpand = argsExpand || f.expands
	}

	if readOnly[base] {
		return safe
	}
	// Past the plain readers, every check reads the flags — and a $VAR or a
	// glob could expand into the very flag a check refuses.
	_, inspected := argChecks[base]
	_, sub := subcommands[base]
	if argsExpand && (inspected || sub || base == "dpkg") {
		return confirm("arguments the shell will rewrite")
	}
	switch {
	case sub:
		if reason := checkSubcommand(base, subcommands[base], args); reason != "" {
			return confirm(reason)
		}
		return safe
	case base == "dpkg":
		if len(args) == 0 || !dpkgReadFlags[args[0]] {
			return confirm("dpkg can change installed packages")
		}
		for _, a := range args[1:] {
			if strings.HasPrefix(a, "-") && !dpkgReadFlags[a] {
				return confirm("dpkg " + a + " can change installed packages")
			}
		}
		return safe
	case inspected:
		if reason := argChecks[base](args); reason != "" {
			return confirm(reason)
		}
		return safe
	}
	return confirm("not a known read-only command")
}

// checkSubcommand finds the verb behind the global flags and judges it.
func checkSubcommand(base string, sc subcommand, args []string) string {
	i := 0
	for i < len(args) && strings.HasPrefix(args[i], "-") {
		a := args[i]
		key := a
		if eq := strings.IndexByte(a, '='); eq > 0 {
			key = a[:eq]
		}
		switch {
		case sc.banned[key]:
			return base + " " + key + " points it at other programs"
		case sc.plain[a]:
		case sc.valued[a]:
			i++
		case strings.HasPrefix(a, "--") && strings.Contains(a, "="):
			// --flag=value cannot swallow the verb that follows it.
		default:
			return base + " " + a + " before the subcommand is not understood"
		}
		i++
	}
	if i >= len(args) || !sc.verbs[args[i]] {
		return base + " can change system state"
	}
	if sc.verbArgs != nil {
		return sc.verbArgs(args[i], args[i+1:])
	}
	return ""
}
