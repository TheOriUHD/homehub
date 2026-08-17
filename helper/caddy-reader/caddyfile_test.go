package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

/**
 * The write path, against a stub admin API that can be told to refuse.
 *
 * What is being tested is the ORDER: Caddy decides first, and only a config it
 * has already accepted is allowed to reach the filesystem. Real Caddy accepts
 * every config these tests use, so it could never exercise the refusal.
 */

const sampleCaddyfile = "a.example.com {\n\treverse_proxy 10.0.0.5:8080\n}\n"

// stubCaddy writes a fake `caddy` executable that fails on the named
// subcommands, so the failure paths are reachable.
func stubCaddy(t *testing.T, failOn ...string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "caddy")

	conditions := ""
	for _, sub := range failOn {
		conditions += "if [ \"$1\" = \"" + sub + "\" ]; then echo '{\"level\":\"error\",\"msg\":\"stub refused to " + sub + "\"}' >&2; exit 1; fi\n"
	}

	script := "#!/bin/sh\n" + conditions + "exit 0\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("could not write stub caddy: %v", err)
	}
	return path
}

/**
 * A writable helper in front of a stub admin API.
 *
 * `loadStatus` is a pointer so a test can make Caddy start refusing /load partway
 * through — which is the whole point of the rollback path.
 */
func writableServer(t *testing.T, failOn ...string) (*server, string, *int) {
	t.Helper()
	dir := t.TempDir()
	caddyfile := filepath.Join(dir, "Caddyfile")
	if err := os.WriteFile(caddyfile, []byte(sampleCaddyfile), 0o644); err != nil {
		t.Fatal(err)
	}

	loadStatus := http.StatusOK
	srv := &server{}
	admin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/load" {
			srv.lastContentType = r.Header.Get("Content-Type")
			raw, _ := io.ReadAll(r.Body)
			srv.lastBody = string(raw)
			w.WriteHeader(loadStatus)
			if loadStatus != http.StatusOK {
				_, _ = w.Write([]byte(`{"level":"error","msg":"caddy refused the config"}`))
			}
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(admin.Close)

	*srv = server{
		admin:     admin.URL,
		token:     "a-token-long-enough-to-use",
		client:    &http.Client{},
		caddyfile: caddyfile,
		caddyBin:  stubCaddy(t, failOn...),
		writable:  true,
	}
	return srv, caddyfile, &loadStatus
}

func put(t *testing.T, s *server, body string, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("PUT", "/caddyfile", strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)
	return rec
}

func currentHash(t *testing.T, s *server) string {
	t.Helper()
	payload, err := s.readCaddyfile()
	if err != nil {
		t.Fatal(err)
	}
	return payload.Sha256
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestReadOnlyRefusesToWrite(t *testing.T) {
	s, caddyfile, _ := writableServer(t)
	s.writable = false

	rec := put(t, s, `{"text":"b.example.com {\n\treverse_proxy 10.0.0.6:80\n}\n"}`, s.token)
	if rec.Code != http.StatusForbidden {
		t.Errorf("got %d, want 403", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "--writable") {
		t.Errorf("the refusal should say how to enable it: %s", rec.Body.String())
	}
	if read(t, caddyfile) != sampleCaddyfile {
		t.Error("the Caddyfile was changed by a read-only helper")
	}
}

func TestWritingNeedsTheToken(t *testing.T) {
	s, caddyfile, _ := writableServer(t)

	if code := put(t, s, `{"text":"x"}`, "").Code; code != http.StatusUnauthorized {
		t.Errorf("no token: got %d, want 401", code)
	}
	if code := put(t, s, `{"text":"x"}`, "wrong").Code; code != http.StatusUnauthorized {
		t.Errorf("wrong token: got %d, want 401", code)
	}
	if read(t, caddyfile) != sampleCaddyfile {
		t.Error("an unauthorised request changed the file")
	}
}

func TestAValidChangeIsWrittenAndBackedUp(t *testing.T) {
	s, caddyfile, _ := writableServer(t)
	updated := "a.example.com {\n\treverse_proxy 10.0.0.5:9999\n}\n"

	body, _ := json.Marshal(replaceRequest{Text: updated, Sha256: currentHash(t, s)})
	rec := put(t, s, string(body), s.token)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	if read(t, caddyfile) != updated {
		t.Error("the new configuration was not written")
	}

	backups, _ := filepath.Glob(caddyfile + ".devtracker-*.bak")
	if len(backups) != 1 {
		t.Fatalf("expected one backup, got %d", len(backups))
	}
	if read(t, backups[0]) != sampleCaddyfile {
		t.Error("the backup does not contain what was replaced")
	}
}

func TestAnInvalidChangeNeverTouchesTheFile(t *testing.T) {
	// Caddy is asked first. Nothing on disk moves until it has accepted the
	// replacement, so a rejected config is a non-event rather than something to
	// undo.
	s, caddyfile, loadStatus := writableServer(t)
	*loadStatus = http.StatusBadRequest

	body, _ := json.Marshal(replaceRequest{Text: "this is not a caddyfile", Sha256: currentHash(t, s)})
	rec := put(t, s, string(body), s.token)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if read(t, caddyfile) != sampleCaddyfile {
		t.Error("an invalid configuration reached the Caddyfile")
	}
	if backups, _ := filepath.Glob(caddyfile + ".devtracker-*.bak"); len(backups) != 0 {
		t.Error("a rejected change still made a backup")
	}
	if !strings.Contains(rec.Body.String(), "nothing was changed") {
		t.Errorf("the error should say nothing changed: %s", rec.Body.String())
	}
}

func TestNothingIsLeftBesideTheCaddyfile(t *testing.T) {
	s, caddyfile, loadStatus := writableServer(t)
	*loadStatus = http.StatusBadRequest
	body, _ := json.Marshal(replaceRequest{Text: "nope", Sha256: currentHash(t, s)})
	put(t, s, string(body), s.token)

	// A stray candidate next to the Caddyfile would be picked up by an
	// `import *.caddy` glob, which is a fine way to load a config nobody
	// approved. There is no temporary file at all now, and this keeps it that
	// way.
	leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(caddyfile), ".devtracker-*"))
	if len(leftovers) != 0 {
		t.Errorf("left %d files behind: %v", len(leftovers), leftovers)
	}
}

func TestARefusedConfigMakesNoBackupEither(t *testing.T) {
	// There is nothing to roll back from, so there is nothing to keep a copy
	// for. A backup per rejected attempt would bury the useful ones.
	s, caddyfile, loadStatus := writableServer(t)
	*loadStatus = http.StatusBadRequest

	body, _ := json.Marshal(replaceRequest{
		Text:   "a.example.com {\n\treverse_proxy 10.0.0.5:9999\n}\n",
		Sha256: currentHash(t, s),
	})
	put(t, s, string(body), s.token)

	if backups, _ := filepath.Glob(caddyfile + ".devtracker-*.bak"); len(backups) != 0 {
		t.Errorf("a refused change still made %d backup(s)", len(backups))
	}
}

func TestCaddyAdaptsTheTextItself(t *testing.T) {
	// The point of the whole rewrite: Caddy is handed the Caddyfile, not JSON
	// adapted here. That is what lets a config using {env.SOMETHING} work
	// without this program holding a copy of the secret.
	s, _, _ := writableServer(t)

	body, _ := json.Marshal(replaceRequest{Text: sampleCaddyfile, Sha256: currentHash(t, s)})
	if rec := put(t, s, string(body), s.token); rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}

	if s.lastContentType != "text/caddyfile" {
		t.Errorf("posted as %q, want text/caddyfile — Caddy has to do the adapting",
			s.lastContentType)
	}
	if !strings.Contains(s.lastBody, "reverse_proxy") {
		t.Errorf("posted %q, which is not the Caddyfile text", s.lastBody)
	}
}

func TestAStaleEditIsRefused(t *testing.T) {
	// Someone edited the file by hand while a browser tab held an older copy.
	// Writing the tab's version would silently drop their edit.
	s, caddyfile, _ := writableServer(t)

	body, _ := json.Marshal(replaceRequest{Text: "b.example.com {\n}\n", Sha256: "not-the-current-hash"})
	rec := put(t, s, string(body), s.token)

	if rec.Code != http.StatusConflict {
		t.Fatalf("got %d, want 409", rec.Code)
	}
	if read(t, caddyfile) != sampleCaddyfile {
		t.Error("a stale write went through anyway")
	}
	if !strings.Contains(rec.Body.String(), "edited it by hand") {
		t.Errorf("the conflict should explain itself: %s", rec.Body.String())
	}
}

func TestAnEmptyCaddyfileIsRefused(t *testing.T) {
	// Valid, and takes every site offline. Not something to do by accident.
	s, caddyfile, _ := writableServer(t)

	body, _ := json.Marshal(replaceRequest{Text: "   \n", Sha256: currentHash(t, s)})
	if code := put(t, s, string(body), s.token).Code; code != http.StatusBadRequest {
		t.Errorf("got %d, want 400", code)
	}
	if read(t, caddyfile) != sampleCaddyfile {
		t.Error("the Caddyfile was emptied")
	}
}

func TestTheFileModeIsPreserved(t *testing.T) {
	s, caddyfile, _ := writableServer(t)
	if err := os.Chmod(caddyfile, 0o600); err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(replaceRequest{
		Text:   "a.example.com {\n\treverse_proxy 10.0.0.5:9999\n}\n",
		Sha256: currentHash(t, s),
	})
	put(t, s, string(body), s.token)

	info, err := os.Stat(caddyfile)
	if err != nil {
		t.Fatal(err)
	}
	// Widening a deliberately private config file would be a quiet regression.
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode became %v, want 0600", info.Mode().Perm())
	}
}

func TestBackupsArePruned(t *testing.T) {
	_, caddyfile, _ := writableServer(t)
	dir := filepath.Dir(caddyfile)

	for i := 0; i < keepBackups+5; i++ {
		name := filepath.Join(dir, "Caddyfile.devtracker-2026010"+string(rune('0'+i%10))+"-00000"+string(rune('0'+i%10))+".bak")
		if err := os.WriteFile(name, []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	pruneBackups(caddyfile)

	left, _ := filepath.Glob(caddyfile + ".devtracker-*.bak")
	if len(left) > keepBackups {
		t.Errorf("kept %d backups, want at most %d", len(left), keepBackups)
	}
}

func TestReadingTheCaddyfileNeedsTheTokenToo(t *testing.T) {
	s, _, _ := writableServer(t)

	req := httptest.NewRequest("GET", "/caddyfile", nil)
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("got %d, want 401", rec.Code)
	}

	req = httptest.NewRequest("GET", "/caddyfile", nil)
	req.Header.Set("Authorization", "Bearer "+s.token)
	rec = httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)

	var payload caddyfilePayload
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if payload.Text != sampleCaddyfile {
		t.Error("did not return the file")
	}
	if payload.Sha256 == "" || !payload.Writable {
		t.Error("the response should carry the hash and the write mode")
	}
}

func TestRepeatedWarningsDoNotBuryTheError(t *testing.T) {
	// What a fifteen-site config actually produced: the same lint warning once
	// per site, and the real failure on the last line.
	var lines []string
	for i := 0; i < 12; i++ {
		lines = append(lines,
			`{"level":"warn","msg":"Unnecessary header_up X-Forwarded-For: the reverse proxy's default behavior is to pass headers"}`,
			`{"level":"warn","msg":"Unnecessary header_up X-Forwarded-Proto: the reverse proxy's default behavior is to pass headers"}`)
	}
	lines = append(lines,
		`{"level":"error","msg":"loading http app module: provision http: getting tls app: loading module 'cloudflare': API token '' appears invalid"}`)

	out := tidyCaddyOutput(strings.Join(lines, "\n"), "/tmp/x")

	if strings.Count(out, "Unnecessary header_up X-Forwarded-For") != 1 {
		t.Errorf("the same warning was not collapsed:\n%s", out)
	}
	if !strings.HasPrefix(out, "API token '' appears invalid") {
		// The reason has to be the first thing read, not the twenty-fifth.
		t.Errorf("the error is not first:\n%s", out)
	}
	if lines := strings.Count(out, "\n"); lines > 6 {
		t.Errorf("still %d lines long:\n%s", lines+1, out)
	}
}

func TestCaddyOutputIsMadeReadable(t *testing.T) {
	raw := `{"level":"error","ts":1,"msg":"unrecognized directive: reverse_prox","file":"/tmp/.devtracker-candidate-1"}
{"level":"info","ts":1,"msg":"noise nobody needs"}`

	out := tidyCaddyOutput(raw, "/tmp/.devtracker-candidate-1")
	if !strings.Contains(out, "unrecognized directive") {
		t.Errorf("lost the actual reason: %s", out)
	}
	if strings.Contains(out, "noise nobody needs") {
		t.Errorf("kept info-level noise: %s", out)
	}
	if strings.Contains(out, "devtracker-candidate") {
		t.Errorf("leaked the temporary filename, which means nothing to the reader: %s", out)
	}
}

func TestTheUnitSaysWhetherItCanWrite(t *testing.T) {
	// `systemctl cat caddy-reader` must answer "can this rewrite my config"
	// on its own. Routing the flag through an environment variable meant the
	// answer lived in two files and could disagree with itself.
	writable := unitFile(true, nil)
	if !strings.Contains(writable, "ExecStart=/usr/local/bin/caddy-reader --writable") {
		t.Errorf("write mode is not visible in ExecStart:\n%s", writable)
	}
	if strings.Contains(writable, "$CADDY_READER_FLAGS") {
		t.Error("the flag is still indirected through a variable")
	}

	readonly := unitFile(false, nil)
	if strings.Contains(readonly, "--writable") {
		t.Error("read-only mode still passes --writable")
	}
	// And the sandbox differs, which is the actual privilege boundary.
	if !strings.Contains(readonly, "ReadOnlyPaths=/") || strings.Contains(readonly, "ReadWritePaths") {
		t.Error("the read-only unit is not actually locked down")
	}
	if !strings.Contains(writable, "ReadWritePaths=/etc/caddy") {
		t.Error("the writable unit cannot write where it needs to")
	}
}

func TestReinstallingKeepsWriteAccess(t *testing.T) {
	// The failure this test exists for: someone presses Enter through an
	// upgrade, and the manager silently goes read-only until the next edit
	// fails in a browser.
	dir := t.TempDir()
	unit := filepath.Join(dir, "caddy-reader.service")
	if err := os.WriteFile(unit, []byte(unitFile(true, nil)), 0o644); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(unit)
	writable := false
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "ExecStart=") && strings.Contains(line, "--writable") {
			writable = true
		}
	}
	if !writable {
		t.Error("an installed writable unit does not read back as writable")
	}
}
