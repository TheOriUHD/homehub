// Package config loads bootstrap configuration from the environment.
//
// Only bootstrap-level concerns live here (where data lives, how to reach the
// database, what to listen on, the secret-wrapping key). Everything else is
// runtime configuration managed inside the app.
package config

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	// DataDir holds the generated master key and any local working files.
	DataDir string
	// HTTPAddr is the listen address, e.g. ":8080".
	HTTPAddr string
	// DatabaseURL is the PostgreSQL connection string.
	DatabaseURL string
	// MasterKey optionally supplies the base64-encoded 32-byte secret-wrapping
	// key. When empty, a key is generated and persisted in DataDir.
	MasterKey string
	// LogLevel is the minimum slog level.
	LogLevel slog.Level
	// TrustedProxies are the CIDRs (or bare IPs) of reverse proxies whose
	// X-Forwarded-For / X-Real-Ip headers may be believed. Empty (the default)
	// means trust none: the client IP is the direct socket peer, so a LAN
	// caller cannot forge its source IP for the rate-limiter, audit log, or
	// session list. Set HOMEHUB_TRUSTED_PROXIES only when running behind a
	// reverse proxy.
	TrustedProxies []*net.IPNet
}

func Load() (*Config, error) {
	cfg := &Config{
		DataDir:     envOr("HOMEHUB_DATA_DIR", "./data"),
		DatabaseURL: envOr("HOMEHUB_DATABASE_URL", "postgres://homehub:homehub@localhost:5432/homehub?sslmode=disable"),
		MasterKey:   os.Getenv("HOMEHUB_MASTER_KEY"),
	}

	if addr := os.Getenv("HOMEHUB_ADDR"); addr != "" {
		cfg.HTTPAddr = addr
	} else {
		cfg.HTTPAddr = ":" + envOr("HOMEHUB_PORT", "8080")
	}

	switch strings.ToLower(envOr("HOMEHUB_LOG_LEVEL", "info")) {
	case "debug":
		cfg.LogLevel = slog.LevelDebug
	case "info":
		cfg.LogLevel = slog.LevelInfo
	case "warn", "warning":
		cfg.LogLevel = slog.LevelWarn
	case "error":
		cfg.LogLevel = slog.LevelError
	default:
		return nil, fmt.Errorf("invalid HOMEHUB_LOG_LEVEL %q (debug|info|warn|error)", os.Getenv("HOMEHUB_LOG_LEVEL"))
	}

	abs, err := filepath.Abs(cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("resolve data dir: %w", err)
	}
	cfg.DataDir = abs
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create data dir %s: %w", cfg.DataDir, err)
	}

	if raw := strings.TrimSpace(os.Getenv("HOMEHUB_TRUSTED_PROXIES")); raw != "" {
		nets, err := parseTrustedProxies(raw)
		if err != nil {
			return nil, err
		}
		cfg.TrustedProxies = nets
	}
	return cfg, nil
}

// parseTrustedProxies accepts a comma-separated list of CIDRs and bare IPs
// (bare IPs become /32 or /128) and returns the parsed networks.
func parseTrustedProxies(raw string) ([]*net.IPNet, error) {
	var nets []*net.IPNet
	for _, tok := range strings.Split(raw, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		if !strings.Contains(tok, "/") {
			if ip := net.ParseIP(tok); ip != nil {
				bits := 32
				if ip.To4() == nil {
					bits = 128
				}
				tok = fmt.Sprintf("%s/%d", tok, bits)
			}
		}
		_, n, err := net.ParseCIDR(tok)
		if err != nil {
			return nil, fmt.Errorf("invalid HOMEHUB_TRUSTED_PROXIES entry %q: %w", tok, err)
		}
		nets = append(nets, n)
	}
	return nets, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
