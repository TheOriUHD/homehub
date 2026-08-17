package main

// `caddy-reader install` — set this up as a systemd service, on the box.
//
// A subcommand rather than a separate script because the binary is already here:
// copying a second file over to install the first one is a step that exists only
// to make the instructions longer.
//
// It reads the running Caddy service for everything it can, shows what it found,
// and then CHECKS THAT THE RESULT WORKS before claiming success. That check
// matters more than the questions: an earlier version reported "Running" while
// the helper could not validate a single change, because Caddy's API token was
// not in its environment.

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"net/http"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	unitPath = "/etc/systemd/system/caddy-reader.service"
	envPath  = "/etc/caddy-reader.env"
)

type detected struct {
	service   string
	caddyfile string
	caddyBin  string
	caddyVer  string
	envFiles  []string
	/** Inline `Environment=` on Caddy's unit — the other place a token hides. */
	inlineEnv []string
	blocks    int
}

/** Ask systemd what the running Caddy is actually doing. */
func detectCaddy() detected {
	d := detected{caddyfile: "/etc/caddy/Caddyfile", caddyBin: "caddy"}

	for _, name := range []string{"caddy", "caddy-server"} {
		out, err := exec.Command("systemctl", "show", name,
			"--property=ExecStart", "--property=EnvironmentFiles",
			"--property=Environment", "--property=LoadState").Output()
		if err != nil || !strings.Contains(string(out), "LoadState=loaded") {
			continue
		}
		d.service = name
		text := string(out)

		if m := regexp.MustCompile(`--config\s+(\S+)`).FindStringSubmatch(text); len(m) == 2 {
			d.caddyfile = strings.Trim(m[1], `"`)
		}
		if m := regexp.MustCompile(`path=(\S+?)\s`).FindStringSubmatch(text); len(m) == 2 {
			d.caddyBin = m[1]
		}
		for _, m := range regexp.MustCompile(`EnvironmentFiles=(\S+)`).FindAllStringSubmatch(text, -1) {
			file := regexp.MustCompile(`\(ignore_errors=(yes|no)\)$`).ReplaceAllString(m[1], "")
			if file != "" {
				d.envFiles = append(d.envFiles, file)
			}
		}
		// `Environment=A=1 B=2`. Missed by an earlier version, and one of the two
		// ways the API token went absent.
		if m := regexp.MustCompile(`(?m)^Environment=(.*)$`).FindStringSubmatch(text); len(m) == 2 {
			for _, pair := range strings.Fields(m[1]) {
				if strings.Contains(pair, "=") {
					d.inlineEnv = append(d.inlineEnv, strings.Trim(pair, `"`))
				}
			}
		}
		break
	}

	if resolved, err := exec.LookPath(d.caddyBin); err == nil {
		d.caddyBin = resolved
	}
	if out, err := exec.Command(d.caddyBin, "version").Output(); err == nil {
		if fields := strings.Fields(strings.TrimSpace(string(out))); len(fields) > 0 {
			d.caddyVer = fields[0]
		}
	}
	if data, err := os.ReadFile(d.caddyfile); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			trimmed := strings.TrimRight(line, " \t")
			if strings.HasSuffix(trimmed, "{") &&
				!strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") &&
				!strings.HasPrefix(strings.TrimSpace(line), "#") &&
				strings.TrimSpace(trimmed) != "{" {
				d.blocks++
			}
		}
	}
	return d
}

/**
 * The variables Caddy runs with.
 *
 * A Caddyfile using {env.CLOUDFLARE_API_TOKEN} cannot be validated without them,
 * and the failure is baffling rather than obvious: Caddy reports the token as
 * INVALID rather than missing, because an empty string is what it was handed.
 */
func (d detected) caddyEnvironment() []string {
	env := append([]string{}, os.Environ()...)
	for _, file := range d.envFiles {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
				continue
			}
			key, value, _ := strings.Cut(line, "=")
			env = append(env, strings.TrimSpace(key)+"="+strings.Trim(strings.TrimSpace(value), `"'`))
		}
	}
	return append(env, d.inlineEnv...)
}

/** Which of Caddy's variables this config actually refers to. */
func placeholdersIn(caddyfile string) []string {
	data, err := os.ReadFile(caddyfile)
	if err != nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, m := range regexp.MustCompile(`\{env\.([A-Za-z_][A-Za-z0-9_]*)\}`).
		FindAllStringSubmatch(string(data), -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	return out
}

func ask(reader *bufio.Reader, prompt, fallback string) string {
	fmt.Printf("     %-22s %s ", prompt, cyan(fallback))
	line, _ := reader.ReadString('\n')
	if line = strings.TrimSpace(line); line != "" {
		return line
	}
	return fallback
}

func askYesNo(reader *bufio.Reader, prompt string, fallback bool) bool {
	hint := "yes / [no]"
	if fallback {
		hint = "[yes] / no"
	}
	fmt.Printf("     %s %s ", prompt, cyan(hint))
	line, _ := reader.ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "":
		return fallback
	case "y", "yes":
		return true
	default:
		return false
	}
}

/**
 * Is the service currently allowed to write?
 *
 * Re-running the installer must not quietly take that away. Someone upgrading
 * presses Enter through the questions -- reasonably, they are confirming what is
 * already true -- and a default of "no" would turn the manager read-only with no
 * indication until the next edit failed in a browser.
 */
func currentlyWritable() bool {
	data, err := os.ReadFile(unitPath)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "ExecStart=") && strings.Contains(line, "--writable") {
			return true
		}
	}
	// The older shape kept it in the environment file.
	env, err := os.ReadFile(envPath)
	return err == nil && strings.Contains(string(env), "--writable")
}

/** Keep the existing token on an upgrade, so devtracker stays connected. */
func existingToken() string {
	data, err := os.ReadFile(envPath)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "READER_TOKEN="); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func newToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func unitFile(writable bool, envFiles []string) string {
	// Written literally rather than through a variable. An operator asking
	// "can this thing change my config" should get the answer from
	// `systemctl cat caddy-reader`, not by cross-referencing an env file.
	exec := "ExecStart=/usr/local/bin/caddy-reader"
	if writable {
		exec += " --writable"
	}

	var b strings.Builder
	b.WriteString(`[Unit]
Description=caddy-reader — devtracker's window onto the local Caddy config
Documentation=https://github.com/TheOriUHD/devtracker/blob/main/helper/README.md
After=network-online.target caddy.service
Wants=network-online.target

[Service]
Type=simple
` + exec + `

# The token lives in a file, not in ExecStart: a unit file is world-readable and
# 'systemctl cat' prints it.
EnvironmentFile=` + envPath + `
`)

	// Caddy's own environment. Without it a config using {env.SOMETHING} cannot
	// be validated, and Caddy calls the resulting empty value an INVALID token
	// rather than a missing one.
	for _, f := range envFiles {
		b.WriteString("EnvironmentFile=-" + f + "\n")
	}

	b.WriteString(`
Restart=on-failure
RestartSec=5s

NoNewPrivileges=yes
PrivateTmp=yes
PrivateDevices=yes
ProtectHome=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes
ProtectClock=yes
ProtectHostname=yes
RestrictNamespaces=yes
RestrictRealtime=yes
RestrictSUIDSGID=yes
LockPersonality=yes
MemoryDenyWriteExecute=yes
SystemCallArchitectures=native
SystemCallFilter=@system-service
SystemCallFilter=~@privileged @resources @obsolete
RestrictAddressFamilies=AF_INET AF_INET6
UMask=0077
`)

	if writable {
		b.WriteString(`
# Write mode. Replacing a file means writing in its DIRECTORY, so the
# read-only-everything sandbox cannot be used and this runs as the user that
# owns the config. Re-run 'caddy-reader setup --read-only' to go back, which
# needs none of it.
User=root
ProtectSystem=full
ReadWritePaths=/etc/caddy
`)
	} else {
		b.WriteString(`
# Read-only: no privileges at all, and nothing on disk is writable.
DynamicUser=yes
ProtectSystem=strict
ReadOnlyPaths=/
CapabilityBoundingSet=
`)
	}

	b.WriteString(`
[Install]
WantedBy=multi-user.target
`)
	return b.String()
}

func envFileText(token, caddyfile, caddyBin, listen, admin string) string {
	return fmt.Sprintf(`# caddy-reader — written by: caddy-reader install
# The token devtracker uses. Treat it like a password.
READER_TOKEN=%s
READER_LISTEN=%s
# Where Caddy's admin API is. The default is right unless you moved it.
CADDY_ADMIN=%s
CADDYFILE=%s
CADDY_BIN=%s
`, token, listen, admin, caddyfile, caddyBin)
}

func primaryAddress() string {
	out, err := exec.Command("sh", "-c",
		"ip -4 route get 1.1.1.1 2>/dev/null | sed -n 's/.*src \\([0-9.]*\\).*/\\1/p'").Output()
	if addr := strings.TrimSpace(string(out)); err == nil && addr != "" {
		return addr
	}
	return "<this-host>"
}

/**
 * `caddy-reader setup`.
 *
 * Three ways to drive it, and all three end in the same place:
 *
 *   setup                          the wizard, Enter accepting every default
 *   setup --yes                    no questions, everything detected
 *   setup --writable --listen …    no questions, everything stated
 *
 * The wizard defaults to what is already configured, so re-running it confirms
 * rather than resets.
 */
func setup(args []string) int {
	assumeYes := false
	writableFlag := currentlyWritable()
	explicitWritable := false

	var overrideCaddyfile, overrideAdmin, overrideListen string

	for i := 0; i < len(args); i++ {
		value := func() string {
			if i+1 < len(args) {
				i++
				return args[i]
			}
			return ""
		}
		switch args[i] {
		case "--writable":
			writableFlag, explicitWritable = true, true
		case "--read-only":
			writableFlag, explicitWritable = false, true
		case "--yes", "-y":
			assumeYes = true
		case "--caddyfile":
			overrideCaddyfile = value()
		case "--admin":
			overrideAdmin = value()
		case "--listen":
			overrideListen = value()
		default:
			fmt.Fprintf(os.Stderr, "Unknown option %q for setup.\n\n", args[i])
			usage()
			return 2
		}
	}
	// Naming any of them means you know what you want; skip the questions.
	if overrideCaddyfile != "" || overrideAdmin != "" || overrideListen != "" || explicitWritable {
		assumeYes = true
	}

	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "Run this with sudo — it writes a systemd unit and starts a service.")
		return 1
	}

	banner("caddy-reader "+Version, "devtracker's window onto your Caddy")

	// --- 1. what is already here ---------------------------------------------

	section("What is already here")
	d := detectCaddy()

	if d.service != "" {
		found(true, "caddy service", d.service, "running")
	} else {
		found(false, "caddy service", "not found", "guessing the usual paths")
	}

	if _, err := os.Stat(d.caddyfile); err != nil {
		failed("Caddyfile", d.caddyfile+" cannot be read")
		fmt.Println()
		note("Nothing was changed. Point it at the right file:\n  caddy-reader install")
		return 1
	}
	blocks := fmt.Sprintf("%d site blocks", d.blocks)
	if d.blocks == 1 {
		blocks = "1 site block"
	}
	found(true, "Caddyfile", d.caddyfile, blocks)

	if _, err := exec.LookPath(d.caddyBin); err == nil {
		found(true, "caddy binary", d.caddyBin, d.caddyVer)
	}

	// Reported, not required. Caddy expands these itself when it is handed the
	// Caddyfile, so this service never needs a copy of any of them.
	if needed := placeholdersIn(d.caddyfile); len(needed) > 0 {
		found(true, "config expands", strings.Join(needed, ", "), "Caddy resolves these, not us")
	}

	// --- 2. confirm ------------------------------------------------------------

	reader := bufio.NewReader(os.Stdin)
	caddyfile, caddyBin := d.caddyfile, d.caddyBin
	listen := valueFromEnvFile("READER_LISTEN", defaultListen)
	admin := valueFromEnvFile("CADDY_ADMIN", env("CADDY_ADMIN", defaultAdmin))
	writable := writableFlag

	if overrideCaddyfile != "" {
		caddyfile = overrideCaddyfile
	}
	if overrideAdmin != "" {
		admin = overrideAdmin
	}
	if overrideListen != "" {
		listen = overrideListen
	}

	if !assumeYes {
		fmt.Println()
		section("Confirm")
		note("Press Enter to keep the value shown, or type a different one.")
		fmt.Println()
		caddyfile = ask(reader, "Caddyfile", caddyfile)
		admin = ask(reader, "Caddy's admin API", admin)
		listen = ask(reader, "listen on", listen)

		fmt.Println()
		section("May devtracker change your sites?")
		note("no    a read-only list. The service gets no privileges at all.\n" +
			"yes   add, edit and remove sites from the browser. The service runs\n" +
			"      as the owner of your Caddy config so it can replace the file.")
		if writableFlag {
			fmt.Println()
			note("It is currently allowed. Enter keeps it that way.")
		}
		fmt.Println()
		writable = askYesNo(reader, "Allow changes?", writableFlag)
	}

	// --- 3. write --------------------------------------------------------------

	fmt.Println()
	section("Setting it up")

	token := existingToken()
	reused := token != ""
	if !reused {
		var err error
		if token, err = newToken(); err != nil {
			failed("token", err.Error())
			return 1
		}
	}

	if err := os.WriteFile(envPath,
		[]byte(envFileText(token, caddyfile, caddyBin, listen, admin)), 0o600); err != nil {
		failed(envPath, err.Error())
		return 1
	}
	found(true, "settings", envPath, "mode 600")

	if err := os.WriteFile(unitPath, []byte(unitFile(writable, d.envFiles)), 0o644); err != nil {
		failed(unitPath, err.Error())
		return 1
	}
	mode := "read-only"
	if writable {
		mode = "writable"
	}
	found(true, "service", unitPath, mode)

	for _, step := range [][]string{
		{"systemctl", "daemon-reload"},
		{"systemctl", "enable", "caddy-reader"},
		{"systemctl", "restart", "caddy-reader"},
	} {
		if out, err := exec.Command(step[0], step[1:]...).CombinedOutput(); err != nil {
			failed(strings.Join(step, " "), strings.TrimSpace(string(out)))
			return 1
		}
	}
	if reused {
		found(true, "token", "kept from last time", "devtracker stays connected")
	} else {
		found(true, "token", "generated", "32 bytes")
	}

	// --- 4. check it actually works -------------------------------------------

	fmt.Println()
	section("Checking it works")
	time.Sleep(1500 * time.Millisecond)

	active, _ := exec.Command("systemctl", "is-active", "caddy-reader").Output()
	if strings.TrimSpace(string(active)) != "active" {
		failed("service", "did not stay running")
		out, _ := exec.Command("journalctl", "-u", "caddy-reader", "-n", "15", "--no-pager", "-o", "cat").Output()
		fmt.Println()
		note(string(out))
		return 1
	}
	found(true, "service", "running", "")

	problems := 0

	// The check that matters. Everything else can be right and this can still
	// fail, and then every change from the browser is rejected for a reason that
	// points at Caddy rather than at the environment it was handed.
	if writable {
		// End to end, using the path a real edit takes: hand Caddy the file it
		// is already serving. It re-applies its own configuration, which proves
		// the whole route works and changes nothing.
		//
		// Note what this does NOT need: the values your config expands. Caddy
		// adapts the text itself, in the process that already has them.
		text, err := os.ReadFile(caddyfile)
		if err == nil {
			probe := &server{admin: admin, client: &http.Client{Timeout: 20 * time.Second}}
			if out, perr := probe.applyCaddyfile(string(text)); perr != nil {
				problems++
				failed("a change is accepted", perr.Error())
				fmt.Println()
				note(tidyCaddyOutput(out, caddyfile))
			} else {
				found(true, "a change is accepted", "yes", "editing will work")
			}
		}
	}

	// Only the NAME of each failed check. verify prints the payload alongside it,
	// which is a page of JSON and turns this summary into the wall of text this
	// whole rewrite exists to stop.
	if out, err := exec.Command(os.Args[0], "verify").CombinedOutput(); err != nil {
		for _, line := range strings.Split(string(out), "\n") {
			name, ok := strings.CutPrefix(strings.TrimSpace(stripANSI(line)), "FAIL")
			if !ok {
				continue
			}
			problems++
			failed("check", clip(name, 44))
		}
		if problems > 0 {
			fmt.Println()
			note("caddy-reader verify prints the detail.")
		}
	}

	// --- 5. what to do with it -------------------------------------------------

	_, port, _ := strings.Cut(listen, ":")
	access := "read-only"
	if writable {
		access = "writable — it can add, edit and remove sites"
	}

	code, err := openPairing(token)
	if err != nil {
		failed("pairing code", err.Error())
		return 1
	}

	panel("In devtracker: Integrations → Connect a Caddy host", [][2]string{
		{"Address", fmt.Sprintf("http://%s:%s", primaryAddress(), port)},
		{"Code   ", bold(spaced(code))},
		{"Access ", access},
	})

	note("The code is good for 15 minutes and can only be used once.\n" +
		"caddy-reader pair     if it expires before you get to it")
	fmt.Println()
	note("caddy-reader status   caddy-reader stop / start   caddy-reader verify")

	fmt.Println()
	if problems > 0 {
		fmt.Printf("  %s\n\n", amber("Running, but the checks above did not all pass."))
		return 1
	}
	fmt.Printf("  %s\n\n", green("Ready."))
	return 0
}

/** Where the installer put things, for anyone wondering what it touched. */
func installPaths() string {
	return fmt.Sprintf("%s\n%s\n%s", unitPath, envPath, filepath.Join("/usr/local/bin", "caddy-reader"))
}
