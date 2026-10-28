package auth

import (
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
)

func TestSetupGate_TokenLifecycle(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	g := NewSetupGate(dir, logger)

	// Before Ensure there is no token, so nothing — including empty — passes.
	if g.Check("") || g.Check("anything") {
		t.Fatal("a gate without a token accepted a guess")
	}
	if err := g.Ensure(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(g.Path())
	if err != nil {
		t.Fatal(err)
	}
	token := strings.TrimSpace(string(b))
	if len(token) < 32 {
		t.Fatalf("token %q is too short for 160 bits", token)
	}
	fi, _ := os.Stat(g.Path())
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("token file mode %v, want 0600", fi.Mode().Perm())
	}
	if g.Check("wrong") || g.Check("") {
		t.Fatal("wrong token accepted")
	}
	// Pasted with stray whitespace / different case still works.
	if !g.Check("  " + strings.ToUpper(token) + "\n") {
		t.Fatal("the real token was rejected")
	}

	// A restart reuses the token the admin may already have copied.
	g2 := NewSetupGate(dir, logger)
	if err := g2.Ensure(); err != nil {
		t.Fatal(err)
	}
	if !g2.Check(token) {
		t.Fatal("token changed across a restart")
	}

	// Once the owner exists the token is retired, file and all.
	g2.Consume()
	if g2.Check(token) {
		t.Fatal("consumed token still accepted")
	}
	if _, err := os.Stat(g2.Path()); !os.IsNotExist(err) {
		t.Fatalf("token file survived Consume: %v", err)
	}
}
