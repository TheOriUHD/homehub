package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"homehub/internal/app"
	"homehub/internal/config"
	"homehub/internal/db"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		if err := migrate(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "homehub:", err)
			os.Exit(1)
		}
		return
	}

	if err := app.Run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "homehub:", err)
		os.Exit(1)
	}
}

// migrate brings the schema up to date without starting the server: for CI,
// and for running migrations as a separate step before a rollout.
func migrate(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))
	pool, err := db.Connect(ctx, cfg.DatabaseURL, logger)
	if err != nil {
		return err
	}
	pool.Close()
	return db.Migrate(ctx, cfg.DatabaseURL, logger)
}
