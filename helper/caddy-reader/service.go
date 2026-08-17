package main

// Running the thing: start, stop, restart, status, uninstall.
//
// Built in rather than left to systemctl, because a small program that installs
// a service and then tells you to go and use a different tool to control it has
// stopped halfway. `systemctl` still works and always will — this is the short
// path, and it can say more than "active (running)".

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func systemctl(args ...string) (string, error) {
	out, err := exec.Command("systemctl", args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func requireRoot() bool {
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "Run this with sudo.")
		return false
	}
	return true
}

func installed() bool {
	_, err := os.Stat(unitPath)
	return err == nil
}

func notInstalled() int {
	fmt.Fprintln(os.Stderr, "Nothing is set up yet. Run: sudo caddy-reader setup")
	return 1
}

func serviceCommand(action string) int {
	if !requireRoot() {
		return 1
	}
	if !installed() {
		return notInstalled()
	}
	if out, err := systemctl(action, "caddy-reader"); err != nil {
		fmt.Fprintf(os.Stderr, "%s failed: %v\n%s\n", action, err, out)
		return 1
	}

	state, _ := systemctl("is-active", "caddy-reader")
	switch state {
	case "active":
		fmt.Printf("  %s  caddy-reader is running\n", green("✓"))
	case "inactive":
		fmt.Printf("  %s  caddy-reader is stopped\n", dim("—"))
	default:
		fmt.Printf("  %s  caddy-reader is %s\n", amber("!"), state)
	}
	return 0
}

/** More than `is-active`: what it is doing, and whether it can change anything. */
func status(_ []string) int {
	if !installed() {
		return notInstalled()
	}

	state, _ := systemctl("is-active", "caddy-reader")
	enabled, _ := systemctl("is-enabled", "caddy-reader")

	word := "running"
	if state != "active" {
		word = state
	}

	access := "read-only"
	if currentlyWritable() {
		access = "writable — HomeHub can change sites"
	}

	banner("caddy-reader "+Version, "")
	found(state == "active", "service", word, "starts on boot: "+enabled)
	found(true, "access", access, "")
	found(true, "Caddyfile", valueFromEnvFile("CADDYFILE", "/etc/caddy/Caddyfile"), "")
	found(true, "listening on", valueFromEnvFile("READER_LISTEN", defaultListen), "")
	found(true, "Caddy admin", valueFromEnvFile("CADDY_ADMIN", defaultAdmin), "")

	if p, err := readPairing(); err == nil {
		found(true, "pairing code", spaced(p.Code), "active")
	}

	fmt.Println()
	if state != "active" {
		note("Logs:  journalctl -u caddy-reader -n 50 --no-pager")
		fmt.Println()
		return 1
	}
	note("caddy-reader verify   check it does what it claims")
	fmt.Println()
	return 0
}

/**
 * Remove everything this program installed.
 *
 * Confirmed by typing a number generated on the spot, rather than "yes". A
 * prompt you can clear by holding Enter is not a confirmation, and this one
 * takes a reverse proxy's management off the machine.
 */
func uninstall(args []string) int {
	if !requireRoot() {
		return 1
	}

	force := false
	for _, a := range args {
		if a == "--yes" || a == "-y" {
			force = true
		}
	}

	banner("Uninstall caddy-reader", "your Caddy configuration is not touched")

	note("This removes:\n" +
		"  " + unitPath + "\n" +
		"  " + envPath + "\n" +
		"  /usr/local/bin/caddy-reader\n\n" +
		"Your Caddyfile, your sites and Caddy itself are left exactly as they are.\n" +
		"HomeHub will report this host as unreachable.")
	fmt.Println()

	if !force {
		code, err := sixDigits()
		if err != nil {
			fmt.Fprintf(os.Stderr, "could not generate a confirmation code: %v\n", err)
			return 1
		}
		fmt.Printf("     To confirm, type %s: ", bold(spaced(code)))
		var typed string
		_, _ = fmt.Scanln(&typed)
		if strings.ReplaceAll(strings.TrimSpace(typed), " ", "") != code {
			fmt.Printf("\n  %s  That did not match. Nothing was removed.\n\n", amber("—"))
			return 1
		}
		fmt.Println()
	}

	if installed() {
		if _, err := systemctl("disable", "--now", "caddy-reader"); err != nil {
			// Worth continuing: a unit that will not stop should still be
			// removed, and systemd is told about it at the end.
			fmt.Printf("  %s  could not stop the service; removing it anyway\n", amber("!"))
		}
	}

	removed, failedTo := 0, 0
	for _, path := range []string{unitPath, envPath, pairPath, "/usr/local/bin/caddy-reader"} {
		if _, err := os.Stat(path); err != nil {
			continue
		}
		if err := os.Remove(path); err != nil {
			fmt.Printf("  %s  %s: %v\n", red("✗"), path, err)
			failedTo++
			continue
		}
		fmt.Printf("  %s  removed %s\n", green("✓"), path)
		removed++
	}

	// Backups are the user's, not ours. Removing someone's only copy of a
	// working config during an uninstall would be unforgivable.
	if matches, _ := filepath.Glob(valueFromEnvFile("CADDYFILE", "/etc/caddy/Caddyfile") + ".homehub-*.bak"); len(matches) > 0 {
		fmt.Println()
		note(fmt.Sprintf("%d Caddyfile backup(s) were left in place. Delete them yourself if\n"+
			"you no longer want them.", len(matches)))
	}

	_, _ = systemctl("daemon-reload")

	fmt.Println()
	if failedTo > 0 {
		fmt.Printf("  %s\n\n", amber("Partly removed. See above."))
		return 1
	}
	fmt.Printf("  %s\n\n", green("Removed."))
	return 0
}
