// Package app wires every subsystem together and owns the process lifecycle.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"

	"homehub/internal/api"
	"homehub/internal/audit"
	"homehub/internal/auth"
	"homehub/internal/config"
	"homehub/internal/db"
	"homehub/internal/httpserver"
	"homehub/internal/jobs"
	"homehub/internal/rbac"
	"homehub/internal/settings"
	"homehub/internal/store"
	"homehub/internal/vault"
	"homehub/internal/version"
	"homehub/internal/ws"
	"homehub/web"
)

// Run boots HomeHub and blocks until ctx is cancelled.
func Run(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(logger)
	logger.Info("starting HomeHub", "version", version.Version, "data_dir", cfg.DataDir)

	if err := db.Migrate(ctx, cfg.DatabaseURL, logger); err != nil {
		return err
	}
	pool, err := db.Connect(ctx, cfg.DatabaseURL, logger)
	if err != nil {
		return err
	}
	defer pool.Close()

	st := store.New(pool)
	v, err := vault.Open(pool, cfg.DataDir, cfg.MasterKey, logger)
	if err != nil {
		return err
	}
	if err := rbac.EnsureSystemRoles(ctx, st); err != nil {
		return err
	}

	settingsSvc := settings.New(st)
	auditSvc := audit.New(st, logger)
	engine := rbac.NewEngine(st)
	authSvc := auth.New(st, v, auditSvc, settingsSvc, logger)
	jobEngine := jobs.New(st, logger, 4)
	registerMaintenance(jobEngine, st, logger)

	hub := ws.NewHub(logger, func(ctx context.Context, userID uuid.UUID, viewAsRole *uuid.UUID, topic string) bool {
		return authorizeTopic(ctx, engine, userID, viewAsRole, topic)
	})

	apiServer := api.NewServer(st, authSvc, engine, auditSvc, settingsSvc, jobEngine, hub, v, logger)
	handler := httpserver.Handler(httpserver.Deps{
		Logger:   logger,
		Auth:     authSvc,
		API:      apiServer,
		Hub:      hub,
		Settings: settingsSvc,
		UI:       web.UI(),
	})
	server := httpserver.NewServer(cfg.HTTPAddr, handler)

	jobsCtx, stopJobs := context.WithCancel(context.Background())
	defer stopJobs()
	jobEngine.Start(jobsCtx)
	if err := ensureMaintenanceSchedule(ctx, st); err != nil {
		logger.Warn("could not ensure maintenance schedule", "error", err)
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", cfg.HTTPAddr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
	}

	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
	stopJobs()
	jobEngine.Wait()
	return nil
}

// authorizeTopic maps WebSocket topics onto permission checks. Unknown
// topics are denied.
func authorizeTopic(ctx context.Context, engine *rbac.Engine, userID uuid.UUID, viewAsRole *uuid.UUID, topic string) bool {
	principal := rbac.Principal{UserID: userID, ViewAsRole: viewAsRole}
	switch topic {
	case "notifications":
		// Personal inbox — delivered per-user, identity is the authorization.
		return true
	case "health":
		ok, err := engine.Can(ctx, principal, api.ResHealth, rbac.LevelView)
		return err == nil && ok
	default:
		return false
	}
}

// registerMaintenance binds the recurring housekeeping job.
func registerMaintenance(engine *jobs.Engine, st *store.Store, logger *slog.Logger) {
	engine.Register("maintenance.cleanup", func(ctx context.Context, _ json.RawMessage) error {
		if n, err := st.CleanupSessions(ctx); err != nil {
			return fmt.Errorf("cleanup sessions: %w", err)
		} else if n > 0 {
			logger.Info("pruned sessions", "count", n)
		}
		if n, err := st.PruneJobs(ctx, 30*24*time.Hour); err != nil {
			return fmt.Errorf("prune jobs: %w", err)
		} else if n > 0 {
			logger.Info("pruned job history", "count", n)
		}
		if n, err := st.PruneNotifications(ctx, 30*24*time.Hour); err != nil {
			return fmt.Errorf("prune notifications: %w", err)
		} else if n > 0 {
			logger.Info("pruned notifications", "count", n)
		}
		return nil
	})
}

func ensureMaintenanceSchedule(ctx context.Context, st *store.Store) error {
	next, err := jobs.NextRun("13 3 * * *", time.Now())
	if err != nil {
		return err
	}
	return st.EnsureSchedule(ctx, "System maintenance", "maintenance.cleanup", map[string]any{}, "13 3 * * *", next)
}
