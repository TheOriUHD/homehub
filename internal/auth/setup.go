package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base32"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// setupTokenFile is the name of the file in the data directory that holds the
// one-time setup token while the instance has no users.
const setupTokenFile = "setup-token"

// SetupGate guards the unauthenticated first-run wizard. Without it the first
// Owner account belongs to whoever reaches the port first — on a LAN that is
// anyone. The token proves the person completing setup can read the server's
// log or data directory, i.e. is the one who installed it.
type SetupGate struct {
	dir    string
	logger *slog.Logger

	mu    sync.Mutex
	token string
}

// NewSetupGate returns a gate that keeps its token file in dir.
func NewSetupGate(dir string, logger *slog.Logger) *SetupGate {
	return &SetupGate{dir: dir, logger: logger.With("component", "setup")}
}

// Path is where the token is written, shown in the wizard so the admin knows
// where to look.
func (g *SetupGate) Path() string { return filepath.Join(g.dir, setupTokenFile) }

// Ensure makes sure a token exists, generating, logging and persisting one if
// needed. Called at startup and whenever the wizard is reachable (an instance
// whose last user was deleted needs one too). A token left from a previous
// run is reused so a restart doesn't invalidate what the admin already
// copied.
func (g *SetupGate) Ensure() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.token != "" {
		return nil
	}
	if b, err := os.ReadFile(g.Path()); err == nil {
		if t := strings.TrimSpace(string(b)); len(t) >= 20 {
			g.token = t
			g.announce()
			return nil
		}
	}
	raw := make([]byte, 20) // 160 bits
	if _, err := rand.Read(raw); err != nil {
		return err
	}
	t := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw))
	// 0600 even though the data dir is 0700: the token is an owner-creation
	// credential and must stay private if the directory is ever loosened.
	// WriteFile keeps an existing file's mode, hence the explicit Chmod.
	if err := os.WriteFile(g.Path(), []byte(t+"\n"), 0o600); err != nil {
		return fmt.Errorf("write setup token: %w", err)
	}
	if err := os.Chmod(g.Path(), 0o600); err != nil {
		return fmt.Errorf("restrict setup token: %w", err)
	}
	g.token = t
	g.announce()
	return nil
}

func (g *SetupGate) announce() {
	g.logger.Warn("HomeHub is not set up yet — open the web UI and enter this setup token to create the owner account",
		"setup_token", g.token, "file", g.Path())
}

// Check reports whether presented matches the current token, in constant
// time. No token (Ensure failed or setup already done) never matches.
func (g *SetupGate) Check(presented string) bool {
	g.mu.Lock()
	t := g.token
	g.mu.Unlock()
	presented = strings.ToLower(strings.TrimSpace(presented))
	if t == "" || presented == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(presented), []byte(t)) == 1
}

// Consume retires the token once the owner exists, so it can't be replayed.
func (g *SetupGate) Consume() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.token = ""
	if err := os.Remove(g.Path()); err != nil && !os.IsNotExist(err) {
		g.logger.Warn("remove setup token file", "error", err)
	}
}
