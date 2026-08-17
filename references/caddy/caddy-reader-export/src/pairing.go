package main

// Pairing: a six-digit code that a person can read out, in exchange for a
// secret a machine can rely on.
//
// The code is NOT the credential. Six digits is a million possibilities, which
// is nothing to a script — so it is short-lived, single-use, and dies after a
// handful of wrong guesses. What it buys is one exchange: devtracker presents
// the code, the helper hands back a real 256-bit token, and the code is burned.
//
// The alternative was what this replaces: reading a 64-character hex string off
// a terminal and typing it into a browser, which people get wrong, and which
// tempts everyone involved into copying it somewhere convenient.

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	/** Long enough to walk to another machine, short enough not to linger. */
	pairWindow = 15 * time.Minute

	/** A million codes and five guesses. Brute force is not the weak point. */
	pairMaxAttempts = 5
)

/**
 * Where the open pairing window lives.
 *
 * Overridable so a test can put it somewhere other than /etc, and so anyone
 * running this outside a normal install is not forced to write there.
 */
var pairPath = env("PAIRING_FILE", "/etc/caddy-reader.pairing")

type pairing struct {
	Code      string    `json:"code"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	Attempts  int       `json:"attempts"`
}

func sixDigits() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	// Zero-padded, so every code is six characters and nobody wonders whether
	// they missed a digit.
	return fmt.Sprintf("%06d", n.Int64()), nil
}

/** Open a pairing window for the given token. Replaces any existing one. */
func openPairing(token string) (string, error) {
	code, err := sixDigits()
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(pairing{
		Code:      code,
		Token:     token,
		ExpiresAt: time.Now().Add(pairWindow),
	})
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(pairPath, data, 0o600); err != nil {
		return "", err
	}
	return code, nil
}

func readPairing() (*pairing, error) {
	data, err := os.ReadFile(pairPath)
	if err != nil {
		return nil, err
	}
	var p pairing
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func closePairing() { _ = os.Remove(pairPath) }

/**
 * Exchange a code for the token.
 *
 * Deliberately the only unauthenticated write-shaped route in this program, and
 * the only one that hands out a secret. Every way it can be abused is closed by
 * the same three things: it expires, it counts wrong guesses, and it burns
 * itself on success.
 */
func (s *server) handlePair(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Body is not JSON."})
		return
	}

	p, err := readPairing()
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "No pairing code is active on this helper. Run 'caddy-reader pair' on it " +
				"to get a new one.",
		})
		return
	}

	if time.Now().After(p.ExpiresAt) {
		closePairing()
		writeJSON(w, http.StatusGone, map[string]string{
			"error": "That pairing code has expired. Run 'caddy-reader pair' for a new one.",
		})
		return
	}

	code := strings.TrimSpace(body.Code)
	if subtle.ConstantTimeCompare([]byte(code), []byte(p.Code)) != 1 {
		p.Attempts++
		if p.Attempts >= pairMaxAttempts {
			// Burn it rather than let someone keep trying. A new code costs one
			// command; a guessed one costs the proxy.
			closePairing()
			writeJSON(w, http.StatusTooManyRequests, map[string]string{
				"error": "Too many wrong codes, so this one has been cancelled. Run " +
					"'caddy-reader pair' on the Caddy host for a new one.",
			})
			return
		}
		if data, err := json.Marshal(p); err == nil {
			_ = os.WriteFile(pairPath, data, 0o600)
		}
		writeJSON(w, http.StatusUnauthorized, map[string]string{
			"error": fmt.Sprintf("Wrong code. %d attempt(s) left before it is cancelled.",
				pairMaxAttempts-p.Attempts),
		})
		return
	}

	// Single use: the window closes the moment it is used, so a code read over
	// someone's shoulder is worth nothing a second time.
	closePairing()
	writeJSON(w, http.StatusOK, map[string]any{
		"token":   p.Token,
		"version": Version,
	})
}

/** `caddy-reader pair` — open a window and print the code. */
func pair(_ []string) int {
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "Run this with sudo — the token is only readable by root.")
		return 1
	}

	token := existingToken()
	if token == "" {
		fmt.Fprintln(os.Stderr, "Nothing is set up yet. Run: sudo caddy-reader setup")
		return 1
	}

	code, err := openPairing(token)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not open a pairing window: %v\n", err)
		return 1
	}

	listen := valueFromEnvFile("READER_LISTEN", defaultListen)
	_, port, _ := strings.Cut(listen, ":")

	banner("Pair with devtracker", "the code is good for 15 minutes")
	panel("In devtracker: Integrations → Connect a Caddy host", [][2]string{
		{"Address", fmt.Sprintf("http://%s:%s", primaryAddress(), port)},
		{"Code   ", spaced(code)},
	})
	note("Single use, and it cancels itself after five wrong guesses.")
	fmt.Println()
	return 0
}

/** 123 456 — easier to read across a room than 123456. */
func spaced(code string) string {
	if len(code) != 6 {
		return code
	}
	return code[:3] + " " + code[3:]
}

/** Read one setting back out of the env file the setup wrote. */
func valueFromEnvFile(key, fallback string) string {
	data, err := os.ReadFile(envPath)
	if err != nil {
		return fallback
	}
	for _, line := range strings.Split(string(data), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), key+"="); ok {
			if v = strings.TrimSpace(v); v != "" {
				return v
			}
		}
	}
	return fallback
}
