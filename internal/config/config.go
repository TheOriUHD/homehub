// Package config loads bootstrap configuration from the environment.
//
// Only bootstrap-level concerns live here (where data lives, how to reach the
// database, what to listen on, the secret-wrapping key). Everything else is
// runtime configuration managed inside the app.
package config

import (
	"fmt"
	"log/slog"
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
	return cfg, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
