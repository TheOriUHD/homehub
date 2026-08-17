package main

// Reading and replacing the Caddyfile.
//
// This is the part that can break a reverse proxy, so the order is chosen to
// make a rejected config a non-event:
//
//	post the NEW text to Caddy   -> Caddy adapts, validates and applies it
//	if Caddy refuses             -> nothing on disk was touched. Done.
//	back up the current file
//	write the new one            -> so it survives a restart
//
// The config is posted as TEXT, with Content-Type: text/caddyfile, so CADDY
// adapts it — in its own process, with its own environment.
//
// That last part is the whole design. An earlier version adapted the file here
// and posted JSON, which meant this program needed every secret the config
// refers to: a Caddyfile saying {env.CLOUDFLARE_API_TOKEN} adapted to JSON
// containing an EMPTY token, and Caddy then rejected it as invalid rather than
// missing. Handing Caddy the text instead means the secrets never leave the
// process that already has them, and this program needs no copy of them, and no
// caddy binary either.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

/** How many timestamped backups to keep beside the Caddyfile. */
const keepBackups = 10

type caddyfilePayload struct {
	Path     string `json:"path"`
	Text     string `json:"text"`
	Sha256   string `json:"sha256"`
	Writable bool   `json:"writable"`
}

type replaceRequest struct {
	Text string `json:"text"`
	/**
	 * The hash of the version being replaced.
	 *
	 * Someone editing the file by hand while a browser tab holds an older copy
	 * is not exotic — it is Tuesday. Without this the browser would win and the
	 * hand edit would vanish.
	 */
	Sha256 string `json:"sha256"`
}

func hashOf(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func (s *server) readCaddyfile() (caddyfilePayload, error) {
	data, err := os.ReadFile(s.caddyfile)
	if err != nil {
		return caddyfilePayload{}, fmt.Errorf("could not read %s: %w", s.caddyfile, err)
	}
	text := string(data)
	return caddyfilePayload{
		Path:     s.caddyfile,
		Text:     text,
		Sha256:   hashOf(text),
		Writable: s.writable,
	}, nil
}

func (s *server) handleGetCaddyfile(w http.ResponseWriter, r *http.Request) {
	if !s.authorised(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Missing or wrong bearer token."})
		return
	}
	payload, err := s.readCaddyfile()
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

/**
 * Hand a Caddyfile to Caddy and let it do the rest.
 *
 * Content-Type picks the adapter, so Caddy parses the text itself. No adapt
 * step here, no temporary file, and no need for this program to hold any of the
 * values the config expands.
 */
func (s *server) applyCaddyfile(text string) (string, error) {
	resp, err := s.client.Post(
		strings.TrimRight(s.admin, "/")+"/load", "text/caddyfile", strings.NewReader(text))
	if err != nil {
		return err.Error(), fmt.Errorf("could not reach Caddy's admin API at %s", s.admin)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != http.StatusOK {
		return string(body), fmt.Errorf("Caddy refused it")
	}
	return "", nil
}

func (s *server) handlePutCaddyfile(w http.ResponseWriter, r *http.Request) {
	if !s.authorised(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Missing or wrong bearer token."})
		return
	}

	if !s.writable {
		writeJSON(w, http.StatusForbidden, map[string]string{
			"error": "This helper is running read-only. Start it with --writable to let HomeHub " +
				"change the Caddyfile, and give it write access to the file.",
		})
		return
	}

	var req replaceRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxConfigBytes)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Body is not JSON: " + err.Error()})
		return
	}
	if strings.TrimSpace(req.Text) == "" {
		// An empty Caddyfile is valid and would take every site offline. If that
		// is genuinely wanted it can be done by hand.
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Refusing to write an empty Caddyfile.",
		})
		return
	}

	current, err := s.readCaddyfile()
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	if req.Sha256 != "" && req.Sha256 != current.Sha256 {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "The Caddyfile changed since you loaded it — someone edited it by hand, or " +
				"another tab saved first. Reload and reapply your change so theirs is not lost.",
		})
		return
	}

	// --- let Caddy decide, before anything on disk moves --------------------
	//
	// A refused config never reaches the filesystem, so there is nothing to undo
	// and no window in which the file and the running proxy disagree.

	if out, err := s.applyCaddyfile(req.Text); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":  "Caddy rejected that configuration, so nothing was changed.",
			"detail": tidyCaddyOutput(out, s.caddyfile),
		})
		return
	}

	// --- Caddy accepted it: now make it survive a restart --------------------

	backup := fmt.Sprintf("%s.homehub-%s.bak", s.caddyfile, time.Now().UTC().Format("20060102-150405"))
	if err := os.WriteFile(backup, []byte(current.Text), 0o600); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "Caddy is serving the change, but the backup could not be saved so the file " +
				"was left alone: " + err.Error() + ". A restart would revert it.",
		})
		return
	}

	// Preserve whatever mode the file already had rather than imposing one.
	mode := os.FileMode(0o644)
	if info, err := os.Stat(s.caddyfile); err == nil {
		mode = info.Mode().Perm()
	}
	if err := os.WriteFile(s.caddyfile, []byte(req.Text), mode); err != nil {
		// Caddy is running the new config and the file still holds the old one.
		// Recoverable, and the operator has to know which way round it is.
		_, _ = s.applyCaddyfile(current.Text)
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "Could not write " + s.caddyfile + ": " + err.Error() +
				". Caddy was put back to the previous configuration, so nothing changed.",
		})
		return
	}

	pruneBackups(s.caddyfile)

	payload, _ := s.readCaddyfile()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "sha256": payload.Sha256, "backup": backup, "version": Version,
	})
}

/**
 * Turn Caddy's output into something worth reading.
 *
 * Caddy logs JSON to stderr, repeats the same lint warning once per site, and
 * puts the actual failure last. A config with fifteen sites produced
 * twenty-four identical "Unnecessary header_up" lines above the one sentence
 * that said what went wrong — which is a way of reporting an error that
 * successfully hides it.
 *
 * So: the error first, warnings deduplicated and counted, and the temporary
 * filename replaced because it means nothing to whoever is reading.
 */
func tidyCaddyOutput(out string, tmpName string) string {
	var errors []string
	var warnings []string
	seen := map[string]bool{}

	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(strings.ReplaceAll(line, tmpName, "Caddyfile"))
		if line == "" {
			continue
		}

		level, msg := "", line
		if strings.HasPrefix(line, "{") {
			var entry map[string]any
			if json.Unmarshal([]byte(line), &entry) == nil {
				text, _ := entry["msg"].(string)
				if text == "" {
					continue
				}
				level, _ = entry["level"].(string)
				msg = text
			}
		}

		isError := level == "error" || strings.HasPrefix(msg, "Error:")
		if isError {
			// The chain of "loading X: loading Y: <reason>" is Caddy narrating
			// its own call stack. The reason is at the end.
			if parts := strings.Split(msg, ": "); len(parts) > 2 {
				msg = strings.TrimPrefix(parts[len(parts)-1], "Error: ")
			}
			if !seen["e:"+msg] {
				seen["e:"+msg] = true
				errors = append(errors, msg)
			}
			continue
		}

		if level == "warn" && !seen["w:"+msg] {
			seen["w:"+msg] = true
			warnings = append(warnings, msg)
		}
	}

	var out2 []string
	out2 = append(out2, errors...)
	if len(warnings) > 0 {
		if len(errors) > 0 {
			out2 = append(out2, "")
		}
		out2 = append(out2, "Caddy also warned:")
		for _, w := range warnings {
			out2 = append(out2, "  "+w)
		}
	}
	if len(out2) == 0 {
		return "Caddy gave no reason."
	}
	return strings.Join(out2, "\n")
}

/** Keep the last few backups; unbounded ones just fill the disk quietly. */
func pruneBackups(caddyfile string) {
	pattern := caddyfile + ".homehub-*.bak"
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) <= keepBackups {
		return
	}
	sort.Strings(matches) // timestamped names sort chronologically
	for _, old := range matches[:len(matches)-keepBackups] {
		_ = os.Remove(old)
	}
}
