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
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/TheOriUHD/homehub/internal/api"
	"github.com/TheOriUHD/homehub/internal/audit"
	"github.com/TheOriUHD/homehub/internal/auth"
	"github.com/TheOriUHD/homehub/internal/config"
	"github.com/TheOriUHD/homehub/internal/db"
	"github.com/TheOriUHD/homehub/internal/httpserver"
	"github.com/TheOriUHD/homehub/internal/integration"
	"github.com/TheOriUHD/homehub/internal/integrations/docker"
	"github.com/TheOriUHD/homehub/internal/integrations/proxmox"
	"github.com/TheOriUHD/homehub/internal/jobs"
	"github.com/TheOriUHD/homehub/internal/metrics"
	"github.com/TheOriUHD/homehub/internal/rbac"
	"github.com/TheOriUHD/homehub/internal/settings"
	"github.com/TheOriUHD/homehub/internal/store"
	"github.com/TheOriUHD/homehub/internal/term"
	"github.com/TheOriUHD/homehub/internal/vault"
	"github.com/TheOriUHD/homehub/internal/version"
	"github.com/TheOriUHD/homehub/internal/ws"
	"github.com/TheOriUHD/homehub/web"
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

	metricsSvc := metrics.New(st, logger)
	jobEngine.Register("metrics.maintenance", func(ctx context.Context, _ json.RawMessage) error {
		return metricsSvc.RunMaintenance(ctx)
	})
	termMgr := term.NewManager()

	manager := integration.NewManager(st, v, metricsSvc, hub, logger)
	manager.Register(proxmox.Driver{})
	manager.Register(docker.Driver{})
	manager.StatusChanged = statusNotifier(st, engine, hub, logger)

	docker.RegisterJobs(func(kind string, fn func(ctx context.Context, payload json.RawMessage) error) {
		jobEngine.Register(kind, fn)
	}, docker.JobDeps{
		Conn: func(id uuid.UUID) (*docker.Conn, error) {
			conn, err := manager.Connection(id)
			if err != nil {
				return nil, err
			}
			dc, ok := conn.(*docker.Conn)
			if !ok {
				return nil, fmt.Errorf("integration %s is not a docker host", id)
			}
			return dc, nil
		},
		RegistryAuth: func(ctx context.Context, instID uuid.UUID, reference string) string {
			host := docker.RegistryHost(reference)
			index := map[string]string{}
			if _, err := st.GetSettingInto(ctx, "docker_registries_"+instID.String(), &index); err != nil {
				return ""
			}
			username, ok := index[host]
			if !ok {
				return ""
			}
			secret, err := v.Open(ctx, "integration:"+instID.String(), "registry:"+host)
			if err != nil {
				return ""
			}
			return docker.EncodeRegistryAuth(username, string(secret), host)
		},
		Notify: func(ctx context.Context, userID uuid.UUID, severity, title, body, resource string) {
			n, err := st.InsertNotification(ctx, &store.Notification{
				UserID:    userID,
				Severity:  severity,
				Title:     title,
				Body:      body,
				Resource:  resource,
				EventType: "docker.job",
			})
			if err != nil {
				logger.Error("insert docker job notification", "error", err)
				return
			}
			hub.PublishUser(userID, "notifications", map[string]any{
				"id": n.ID, "ts": n.TS, "severity": n.Severity, "title": n.Title,
				"body": n.Body, "resource": n.Resource, "eventType": n.EventType, "read": false,
			})
		},
	})

	apiServer := api.NewServer(st, authSvc, engine, auditSvc, settingsSvc, jobEngine, hub, v,
		manager, metricsSvc, termMgr, logger)
	handler := httpserver.Handler(httpserver.Deps{
		Logger:   logger,
		Auth:     authSvc,
		API:      apiServer,
		Hub:      hub,
		Settings: settingsSvc,
		Term:     termMgr,
		UI:       web.UI(),
	})
	server := httpserver.NewServer(cfg.HTTPAddr, handler)

	jobsCtx, stopJobs := context.WithCancel(context.Background())
	defer stopJobs()
	jobEngine.Start(jobsCtx)
	if err := ensureMaintenanceSchedule(ctx, st); err != nil {
		logger.Warn("could not ensure maintenance schedule", "error", err)
	}
	if err := manager.Start(ctx); err != nil {
		return err
	}
	defer manager.Stop()

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
	switch {
	case topic == "notifications":
		// Personal inbox — delivered per-user, identity is the authorization.
		return true
	case topic == "health":
		ok, err := engine.Can(ctx, principal, api.ResHealth, rbac.LevelView)
		return err == nil && ok
	case topic == "integrations":
		// Admin surface: the integrations management page.
		ok, err := engine.Can(ctx, principal, api.ResIntegrations, rbac.LevelConfigure)
		return err == nil && ok
	case strings.HasPrefix(topic, "integration:"), strings.HasPrefix(topic, "metrics:"):
		// Instance topics carry the resource path after the colon.
		_, resource, _ := strings.Cut(topic, ":")
		ok, err := engine.VisibleUnder(ctx, principal, resource)
		return err == nil && ok
	default:
		return false
	}
}

// statusNotifier turns integration status edges into notifications for every
// user who can see the instance. The reconnect loop cycles
// connecting→unreachable on every retry, so notifications key off the last
// *notified* state per instance — one alert per outage, one per recovery.
func statusNotifier(st *store.Store, engine *rbac.Engine, hub *ws.Hub, logger *slog.Logger) func(integration.Instance, integration.Status, integration.Status, string) {
	var mu sync.Mutex
	lastNotified := map[uuid.UUID]integration.Status{}

	return func(inst integration.Instance, from, to integration.Status, message string) {
		if to != integration.StatusUnreachable && to != integration.StatusConnected {
			return
		}
		mu.Lock()
		previous := lastNotified[inst.ID]
		shouldNotify := (to == integration.StatusUnreachable && previous != integration.StatusUnreachable && from != integration.StatusPending) ||
			(to == integration.StatusConnected && previous == integration.StatusUnreachable)
		if shouldNotify {
			lastNotified[inst.ID] = to
		}
		mu.Unlock()
		if !shouldNotify {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		severity, title := "error", inst.Name+" is unreachable"
		body := message
		eventType := "integration.unreachable"
		if to == integration.StatusConnected {
			severity, title = "success", inst.Name+" is back online"
			body = ""
			eventType = "integration.recovered"
		}

		users, err := st.ListUsers(ctx)
		if err != nil {
			logger.Error("notify integration status: list users", "error", err)
			return
		}
		for _, user := range users {
			if user.Status != "active" {
				continue
			}
			visible, err := engine.VisibleUnder(ctx, rbac.Principal{UserID: user.ID}, inst.ResourcePath())
			if err != nil || !visible {
				continue
			}
			n, err := st.InsertNotification(ctx, &store.Notification{
				UserID:    user.ID,
				Severity:  severity,
				Title:     title,
				Body:      body,
				Resource:  inst.ResourcePath(),
				EventType: eventType,
			})
			if err != nil {
				logger.Error("insert integration notification", "error", err)
				continue
			}
			hub.PublishUser(user.ID, "notifications", map[string]any{
				"id": n.ID, "ts": n.TS, "severity": n.Severity, "title": n.Title,
				"body": n.Body, "resource": n.Resource, "eventType": n.EventType, "read": false,
			})
		}
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
	if err := st.EnsureSchedule(ctx, "System maintenance", "maintenance.cleanup", map[string]any{}, "13 3 * * *", next); err != nil {
		return err
	}
	next, err = jobs.NextRun("*/5 * * * *", time.Now())
	if err != nil {
		return err
	}
	return st.EnsureSchedule(ctx, "Metric rollups", "metrics.maintenance", map[string]any{}, "*/5 * * * *", next)
}
