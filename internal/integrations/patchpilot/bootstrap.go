package patchpilot

import (
	"context"
	"fmt"
	"strings"
)

// The managed account created on every host. Least privilege: it may run
// exactly the package-manager and reboot binaries, by absolute path.
const ManagedUser = "patchpilot"

// BootstrapStep is one progress event streamed to the UI during onboarding.
type BootstrapStep struct {
	Step    string `json:"step"`
	Status  string `json:"status"` // running | ok | error
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

// BootstrapResult is what a successful privileged install yields.
type BootstrapResult struct {
	Paths        map[string]string
	SudoersEntry string
}

// BootstrapError carries a machine code, a human summary, and actionable
// guidance — never a bare "exited with status 1" (spec §7.7).
type BootstrapError struct {
	Code     string
	Summary  string
	Guidance string
	Output   string
}

func (e *BootstrapError) Error() string {
	if e.Guidance != "" {
		return e.Summary + " — " + e.Guidance
	}
	return e.Summary
}

func bootErr(code, summary, guidance, output string) *BootstrapError {
	return &BootstrapError{Code: code, Summary: summary, Guidance: guidance, Output: output}
}

// commandPathResolutionScript resolves each needed binary to an absolute path
// on the target, printing `name=/abs/path` (or `name=` when unresolved).
const commandPathResolutionScript = `
export PATH=/usr/sbin:/usr/bin:/sbin:/bin:$PATH
resolve() {
  p=$(command -v -p "$1" 2>/dev/null || command -v "$1" 2>/dev/null)
  # command -v returns the bare name for shell builtins (e.g. test) — only
  # trust an absolute path; otherwise search the canonical directories.
  case "$p" in
    /*) ;;
    *) p=''; for d in /usr/sbin /usr/bin /sbin /bin; do
         if [ -x "$d/$1" ]; then p="$d/$1"; break; fi
       done ;;
  esac
  echo "$1=$p"
}
for c in %s; do resolve "$c"; done
`

// allNeededCommands is every binary the sudoers file or bootstrap steps use.
func allNeededCommands(mgr string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(names ...string) {
		for _, n := range names {
			if !seen[n] {
				seen[n] = true
				out = append(out, n)
			}
		}
	}
	add(managerBinaries(mgr)...)
	add("test", "systemctl", "reboot", "shutdown")
	if mgr == MgrDNF {
		add("needs-restarting")
	}
	// Admin tools used to create the account + write files.
	add("id", "install", "useradd", "adduser", "visudo", "rm", "chmod", "chown", "sh")
	return out
}

// resolvePaths runs the resolution script (as the login user — no root needed)
// and returns the name→path map.
func resolvePaths(ctx context.Context, conn *SSHConn, mgr string) (map[string]string, error) {
	cmds := strings.Join(allNeededCommands(mgr), " ")
	// Under /bin/sh (not the login shell), so a non-POSIX login shell can't
	// break the resolution script.
	out, _, err := conn.RunScript(ctx, fmt.Sprintf(commandPathResolutionScript, cmds))
	if err != nil {
		return nil, err
	}
	paths := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			paths[k] = v
		}
	}
	return paths, nil
}

// hardenedAuthorizedKey wraps the public key with restrictive options and an
// optional from= source-IP pin (§7.5).
func hardenedAuthorizedKey(pubKey, sourceIP string) string {
	opts := "no-pty,no-X11-forwarding,no-agent-forwarding,no-port-forwarding"
	if sourceIP != "" {
		opts = fmt.Sprintf("from=\"%s\",%s", sourceIP, opts)
	}
	return opts + " " + strings.TrimSpace(pubKey)
}

// BootstrapStandard installs the managed account, key, and sudoers drop-in
// over an already-escalating privileged connection. Emits progress via emit.
// On any failure after files are written, it rolls back what it created.
func BootstrapStandard(ctx context.Context, conn *SSHConn, facts *HostFacts, pubKey string, emit func(BootstrapStep)) (*BootstrapResult, error) {
	step := func(name, msg string) { emit(BootstrapStep{Step: name, Status: "running", Message: msg}) }
	ok := func(name, msg string) { emit(BootstrapStep{Step: name, Status: "ok", Message: msg}) }

	mgr := facts.PackageManager
	if mgr == "" {
		return nil, bootErr("command_missing", "no supported package manager on this host",
			"install apt, dnf, pacman, zypper, or apk first", "")
	}

	// 1. If this is a root host without sudo, install it — the managed
	//    account needs the sudoers subsystem to run updates as root.
	if !facts.HasSudo {
		if conn.escalate != EscDirectRoot {
			return nil, bootErr("sudo_missing", "this host has no sudo and you are not root",
				"install sudo on the target, or onboard with root's credentials", "")
		}
		step("sudo-install", "Installing sudo")
		if out, code, err := conn.RunEscalated(ctx, InstallSudoCommand(mgr)); err != nil || code != 0 {
			return nil, bootErr("sudo_install_failed", "could not install sudo", cleanErr(err), out)
		}
		ok("sudo-install", "sudo installed")
	}

	// 2. Resolve absolute paths (after any sudo install, so visudo resolves).
	step("resolve", "Resolving command paths")
	paths, err := resolvePaths(ctx, conn, mgr)
	if err != nil {
		return nil, bootErr("detection_probe", "could not resolve command paths", err.Error(), "")
	}
	for _, req := range RequiredCommands(mgr) {
		if !strings.HasPrefix(paths[req], "/") {
			return nil, bootErr("command_missing", fmt.Sprintf("required command %q not found", req),
				"install it on the target and retry", "")
		}
	}
	ok("resolve", "All required commands resolved")

	// 3. Verify the sudoers subsystem with root privileges (the login user
	//    usually can't read the 0440 /etc/sudoers). We refuse to edit
	//    /etc/sudoers itself — only ever drop a file into /etc/sudoers.d.
	step("sudoers-check", "Checking the sudoers include directory")
	if _, code, _ := conn.RunEscalated(ctx, "test -f /etc/sudoers"); code != 0 {
		return nil, bootErr("sudoers_missing", "/etc/sudoers is missing on this host",
			"this host's sudo is not configured normally; fix it by hand first", "")
	}
	if _, code, _ := conn.RunEscalated(ctx, "grep -qE '^[@#]includedir[[:space:]]+/etc/sudoers.d' /etc/sudoers"); code != 0 {
		return nil, bootErr("sudoers_include_missing", "/etc/sudoers does not include /etc/sudoers.d",
			"add the line '@includedir /etc/sudoers.d' to /etc/sudoers (via visudo) and retry", "")
	}
	ok("sudoers-check", "sudoers.d include is present")

	// 3. Create the managed user (idempotent).
	step("user", "Creating the patchpilot account")
	idBin := path(paths, "id")
	var createUser string
	if strings.HasPrefix(paths["useradd"], "/") {
		createUser = fmt.Sprintf("%s %s >/dev/null 2>&1 || %s --system --create-home --shell /bin/sh %s",
			idBin, ManagedUser, path(paths, "useradd"), ManagedUser)
	} else if strings.HasPrefix(paths["adduser"], "/") { // Alpine
		createUser = fmt.Sprintf("%s %s >/dev/null 2>&1 || %s -D -h /home/%s -s /bin/sh %s",
			idBin, ManagedUser, path(paths, "adduser"), ManagedUser, ManagedUser)
	} else {
		return nil, bootErr("admin_tool_missing", "no useradd/adduser on this host",
			"install shadow-utils (or busybox adduser) and retry", "")
	}
	if out, code, err := conn.RunEscalated(ctx, createUser); err != nil || code != 0 {
		return nil, bootErr("user_creation_failed", "could not create the patchpilot account", cleanErr(err), out)
	}
	ok("user", "Account ready")

	install := path(paths, "install")
	home := "/home/" + ManagedUser

	// 4. .ssh dir + authorized_keys.
	step("authkey", "Installing the SSH key")
	mkSSH := fmt.Sprintf("%s -d -m 0700 -o %s -g %s %s/.ssh", install, ManagedUser, ManagedUser, home)
	if out, code, err := conn.RunEscalated(ctx, mkSSH); err != nil || code != 0 {
		return nil, bootErr("ssh_directory_failed", "could not create ~/.ssh on the target", cleanErr(err), out)
	}
	authLine := hardenedAuthorizedKey(pubKey, conn.SourceIP())
	writeKey := fmt.Sprintf("printf '%%s\\n' %s > %s/.ssh/authorized_keys && chown %s:%s %s/.ssh/authorized_keys && chmod 0600 %s/.ssh/authorized_keys",
		shellSingleQuote(authLine), home, ManagedUser, ManagedUser, home, home)
	if out, code, err := conn.RunEscalated(ctx, writeKey); err != nil || code != 0 {
		_ = rollback(ctx, conn, paths, facts)
		return nil, bootErr("authorized_keys_failed", "could not write authorized_keys", cleanErr(err), out)
	}
	ok("authkey", "Key installed")

	// 5. sudoers.d drop-in + visudo validation.
	step("sudoers", "Writing the least-privilege sudoers rule")
	sudoersBody := SudoersContent(ManagedUser, mgr, paths)
	mkDir := fmt.Sprintf("%s -d -m 0750 /etc/sudoers.d", install)
	if out, code, err := conn.RunEscalated(ctx, mkDir); err != nil || code != 0 {
		_ = rollback(ctx, conn, paths, facts)
		return nil, bootErr("sudoers_write_failed", "could not create /etc/sudoers.d", cleanErr(err), out)
	}
	writeSudoers := fmt.Sprintf("printf '%%s' %s > /etc/sudoers.d/%s && chmod 0440 /etc/sudoers.d/%s",
		shellSingleQuote(sudoersBody), ManagedUser, ManagedUser)
	if out, code, err := conn.RunEscalated(ctx, writeSudoers); err != nil || code != 0 {
		_ = rollback(ctx, conn, paths, facts)
		return nil, bootErr("sudoers_write_failed", "could not write the sudoers drop-in", cleanErr(err), out)
	}
	// Validate; on failure remove the drop-in immediately so we never leave
	// a broken sudoers file behind.
	validate := fmt.Sprintf("%s -cf /etc/sudoers.d/%s", path(paths, "visudo"), ManagedUser)
	if out, code, err := conn.RunEscalated(ctx, validate); err != nil || code != 0 {
		_, _, _ = conn.RunEscalated(ctx, fmt.Sprintf("%s -f /etc/sudoers.d/%s", path(paths, "rm"), ManagedUser))
		_ = rollback(ctx, conn, paths, facts)
		return nil, bootErr("visudo_failed", "the generated sudoers rule failed validation", cleanErr(err), out)
	}
	ok("sudoers", "sudoers rule installed and validated")

	return &BootstrapResult{Paths: paths, SudoersEntry: strings.TrimSpace(sudoersBody)}, nil
}

// VerifyManagedAccess proves the key + NOPASSWD sudoers work end to end, over
// a fresh connection as the managed user. Returns the manager version banner.
func VerifyManagedAccess(ctx context.Context, keyConn *SSHConn, mgr string, paths map[string]string) error {
	mgrBin := path(paths, managerBinaries(mgr)[0])
	cmd := fmt.Sprintf("sudo -n %s --version 2>&1 | head -n 1", mgrBin)
	out, code, err := keyConn.Run(ctx, cmd)
	if err != nil {
		return bootErr("nopasswd_verify_failed", "could not run the package manager as patchpilot", cleanErr(err), out)
	}
	if code != 0 {
		return bootErr("nopasswd_verify_failed", "passwordless sudo did not work for patchpilot", strings.TrimSpace(out), out)
	}
	return nil
}

// rollback removes what a failed bootstrap created — sudoers drop-in, the
// key, and (only if we created it) the account. Never uninstalls sudo.
func rollback(ctx context.Context, conn *SSHConn, paths map[string]string, facts *HostFacts) error {
	rm := path(paths, "rm")
	_, _, _ = conn.RunEscalated(ctx, fmt.Sprintf("%s -f /etc/sudoers.d/%s", rm, ManagedUser))
	_, _, _ = conn.RunEscalated(ctx, fmt.Sprintf("%s -f /home/%s/.ssh/authorized_keys", rm, ManagedUser))
	if !facts.PatchpilotUserExists {
		// Only remove the account we created this run.
		userdel := "userdel"
		if !strings.HasPrefix(paths["userdel"], "/") {
			userdel = "deluser"
		}
		_, _, _ = conn.RunEscalated(ctx, fmt.Sprintf("%s -r %s 2>/dev/null || %s %s 2>/dev/null; true", userdel, ManagedUser, userdel, ManagedUser))
	}
	return nil
}

func cleanErr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
