package main

// A small terminal presentation layer.
//
// No dependency: this binary's whole pitch is that it is one auditable file with
// nothing behind it, and pulling in a TUI framework to draw eight lines would
// trade that away for box corners. Everything here is ANSI escapes and unicode,
// and it degrades to plain text when the output is not a terminal — which is
// what happens when someone pipes the installer into a log.

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"
)

var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

/** Colour codes make a line unsearchable; strip them before matching on it. */
func stripANSI(s string) string { return ansi.ReplaceAllString(s, "") }

var colour = isTerminal()

func isTerminal() bool {
	info, err := os.Stdout.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func sgr(code, text string) string {
	if !colour {
		return text
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}

func bold(s string) string  { return sgr("1", s) }
func dim(s string) string   { return sgr("2", s) }
func green(s string) string { return sgr("32", s) }
func red(s string) string   { return sgr("31", s) }
func amber(s string) string { return sgr("33", s) }
func cyan(s string) string  { return sgr("36", s) }

const width = 66

/** Pad a string that may contain colour codes to a printable width. */
func padVisible(s string, n int) string {
	visible := utf8.RuneCountInString(stripANSI(s))
	if visible >= n {
		return s
	}
	return s + strings.Repeat(" ", n-visible)
}

/** The masthead, drawn once. */
func banner(title, subtitle string) {
	line := strings.Repeat("─", width-2)
	fmt.Printf("\n╭%s╮\n", line)
	fmt.Printf("│ %s │\n", padVisible(bold(title), width-4))
	fmt.Printf("│ %s │\n", padVisible(dim(subtitle), width-4))
	fmt.Printf("╰%s╯\n\n", line)
}

/**
 * A section heading, numbered as it is printed.
 *
 * Counting here rather than at the call site: a run that skips the questions
 * would otherwise print 1, 4, 5 and look like it lost something.
 */
var sectionNo int

func section(title string) {
	sectionNo++
	fmt.Printf("  %s %s\n\n", cyan(fmt.Sprintf("%d", sectionNo)), bold(title))
}

/**
 * One detected fact.
 *
 * The value is what was found; the note says where it came from, because "we
 * guessed" and "we read it off your running service" deserve to look different.
 */
const (
	labelWidth = 24
	valueWidth = 28
)

/**
 * Long values wrap to their own line rather than shoving the note off the edge.
 * A unit path is 40 characters and every column after it was landing crooked.
 */
func row(mark, label, value, trailing string) {
	if len(value) > valueWidth {
		fmt.Printf("     %s  %-*s %s\n", mark, labelWidth, label, value)
		if trailing != "" {
			fmt.Printf("     %s  %-*s %s\n", " ", labelWidth, "", trailing)
		}
		return
	}
	fmt.Printf("     %s  %-*s %-*s %s\n", mark, labelWidth, label, valueWidth, value, trailing)
}

func found(ok bool, label, value, note string) {
	mark := green("✓")
	if !ok {
		mark = amber("!")
	}
	row(mark, label, value, dim(note))
}

func failed(label, why string) {
	row(red("✗"), label, clip(why, 60), "")
}

/** Keep a message to one line; the detail is printed underneath it anyway. */
func clip(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func note(text string) {
	for _, line := range strings.Split(text, "\n") {
		fmt.Printf("     %s\n", dim(line))
	}
}

/** A framed panel for the things someone has to copy out. */
func panel(title string, rows [][2]string) {
	line := strings.Repeat("─", width-2)
	fmt.Printf("\n  ╭%s╮\n", line)
	fmt.Printf("  │ %-*s │\n", width-4, bold(title))
	fmt.Printf("  │ %-*s │\n", width-4, "")
	for _, r := range rows {
		// Width is measured on the text with escape codes removed: they occupy
		// no columns, and counting them pushes the border out of line.
		pad := width - 4 - utf8.RuneCountInString(r[0]) - 2
		if pad < 1 {
			pad = 1
		}
		visible := utf8.RuneCountInString(stripANSI(r[1]))
		padded := r[1] + strings.Repeat(" ", max(0, pad-visible))
		fmt.Printf("  │ %s%s%s │\n", dim(r[0]), strings.Repeat(" ", 2), padded)
	}
	fmt.Printf("  ╰%s╯\n\n", line)
}

/**
 * Pad by CHARACTERS, not bytes.
 *
 * An em-dash is three bytes and one column, so byte-length padding pulls every
 * line containing one two spaces short and the right-hand border goes crooked.
 */
func padTo(s string, n int) string {
	w := utf8.RuneCountInString(s)
	if w >= n {
		return s
	}
	return s + strings.Repeat(" ", n-w)
}
