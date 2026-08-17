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
	"syscall"
	"time"

	"github.com/google/uuid"

	"homehub/companion"
	"homehub/internal/alerting"
	"homehub/internal/api"
	"homehub/internal/audit"
	"homehub/internal/auth"
	"homehub/internal/backup"
	"homehub/internal/config"
	"homehub/internal/db"
	"homehub/internal/dbdump"
	"homehub/internal/httpserver"
	"homehub/internal/integration"
	"homehub/internal/integrations/adguard"
	"homehub/internal/integrations/amp"
	"homehub/internal/integrations/dbsql"
	"homehub/internal/integrations/docker"
	"homehub/internal/integrations/homeassistant"
	"homehub/internal/integrations/minecraft"
	"homehub/internal/integrations/mongodb"
	"homehub/internal/integrations/pihole"
	"homehub/internal/integrations/proxmox"
	"homehub/internal/integrations/truenas"
	"homehub/internal/jobs"
	"homehub/internal/metrics"
	"homehub/internal/notify"
	"homehub/internal/rbac"
	"homehub/internal/settings"
	"homehub/internal/store"
	"homehub/internal/term"
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

	metricsSvc := metrics.New(st, settingsSvc.MetricsRetention, logger)
	jobEngine.Register("metrics.maintenance", func(ctx context.Context, _ json.RawMessage) error {
		return metricsSvc.RunMaintenance(ctx)
	})
	termMgr := term.NewManager()

	notifySvc := notify.New(st, hub, v, logger)
	notifySvc.SetEnqueue(func(ctx context.Context, payload notify.DeliveryPayload) {
		if _, err := jobEngine.Enqueue(ctx, "notify.deliver", payload); err != nil {
			logger.Error("enqueue channel delivery", "error", err)
		}
	})
	jobEngine.Register("notify.deliver", notifySvc.Deliver)

	evaluator := alerting.NewEvaluator(st, notifySvc,
		func(ctx context.Context, userID uuid.UUID, resource string) bool {
			ok, err := engine.VisibleUnder(ctx, rbac.Principal{UserID: userID}, resource)
			return err == nil && ok
		}, logger)
	jobEngine.Register("alerts.evaluate", func(ctx context.Context, _ json.RawMessage) error {
		return evaluator.Run(ctx)
	})

	manager := integration.NewManager(st, v, metricsSvc, hub, logger)
	manager.Register(proxmox.Driver{})
	manager.Register(docker.Driver{})
	manager.Register(pihole.Driver{})
	manager.Register(adguard.Driver{})
	manager.Register(homeassistant.Driver{})
	manager.Register(dbsql.Driver{})
	manager.Register(mongodb.Driver{})
	manager.Register(truenas.Driver{})
	manager.Register(amp.Driver{})
	mcRegistry := minecraft.NewRegistry(st, v, logger)
	mcRegistry.Publish = func(instanceID uuid.UUID, event string, data any) {
		hub.Publish("integration:"+minecraft.TypeID+"/"+instanceID.String(),
			map[string]any{"event": event, "data": data})
	}
	// Companion connects/disconnects nudge the supervisor so the status
	// badge follows within a second instead of the next poll or backoff.
	mcRegistry.StatusChanged = manager.Kick
	manager.Register(minecraft.Driver{Registry: mcRegistry})
	manager.StatusChanged = statusNotifier(engine, notifySvc)

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
			notifySvc.Notify(ctx, userID, severity, title, body, resource, "docker.job")
		},
	})

	backupSvc := backup.NewService(st, v, cfg.DatabaseURL, cfg.DataDir, logger)
	if n, err := st.SweepStaleBackupRuns(ctx); err != nil {
		logger.Error("sweeping stale backup runs failed", "error", err)
	} else if n > 0 {
		logger.Warn("marked interrupted backup runs as failed", "count", n)
	}
	backupSvc.RequestRestart = func() {
		logger.Warn("restart requested after restore")
		_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
	}
	jobEngine.Register("backup.app", func(ctx context.Context, payload json.RawMessage) error {
		var p backup.JobPayload
		if err := json.Unmarshal(payload, &p); err != nil {
			return err
		}
		archive, err := backupSvc.RunAppBackup(ctx, p.TargetID)
		if p.UserID != uuid.Nil {
			if err != nil {
				notifySvc.Notify(ctx, p.UserID, "error", "Backup failed", err.Error(), api.ResBackups, "security")
			} else {
				notifySvc.Notify(ctx, p.UserID, "success", "Backup completed", archive, api.ResBackups, "security")
			}
		}
		return err
	})
	dumpSvc := dbdump.New(st, v, backupSvc, logger)
	dumpSvc.Notify = func(ctx context.Context, userID uuid.UUID, severity, title, body string) {
		notifySvc.Notify(ctx, userID, severity, title, body, api.ResBackups, "security")
	}
	jobEngine.Register("db.dump", dumpSvc.Run)
	jobEngine.Register("backup.restore", func(ctx context.Context, payload json.RawMessage) error {
		var p backup.JobPayload
		if err := json.Unmarshal(payload, &p); err != nil {
			return err
		}
		err := backupSvc.Restore(ctx, p.TargetID, p.Path)
		if err != nil && p.UserID != uuid.Nil {
			notifySvc.Notify(ctx, p.UserID, "error", "Restore failed", err.Error(), api.ResBackups, "security")
		}
		return err
	})

	apiServer := api.NewServer(st, authSvc, engine, auditSvc, settingsSvc, jobEngine, hub, v,
		manager, metricsSvc, termMgr, backupSvc, mcRegistry, companion.FS(), logger)
	handler := httpserver.Handler(httpserver.Deps{
		Logger:   logger,
		Auth:     authSvc,
		API:      apiServer,
		Hub:      hub,
		Settings: settingsSvc,
		Term:     termMgr,
		MC:       mcRegistry,
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
func statusNotifier(engine *rbac.Engine, notifySvc *notify.Service) func(integration.Instance, integration.Status, integration.Status, string) {
	var mu sync.Mutex
	lastNotified := map[uuid.UUID]integration.Status{}

	return func(inst integration.Instance, from, to integration.Status, message string) {
		if to != integration.StatusUnreachable && to != integration.StatusConnected {
			return
		}
		mu.Lock()
		previous := lastNotified[inst.ID]
		if to == integration.StatusConnected && previous == "" {
			// First successful connect this process: remember it silently so a
			// later drop notifies, without announcing every app restart.
			lastNotified[inst.ID] = to
			mu.Unlock()
			return
		}
		// Outage alerts fire only on an observed drop (last notified state was
		// connected) — never for instances that merely failed to connect at
		// boot or are waiting to be paired. Their state is on the Systems page.
		shouldNotify := (to == integration.StatusUnreachable && previous == integration.StatusConnected) ||
			(to == integration.StatusConnected && previous == integration.StatusUnreachable)
		if shouldNotify {
			lastNotified[inst.ID] = to
		}
		mu.Unlock()
		if !shouldNotify {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		severity, title := "error", inst.Name+" is unreachable"
		body := message
		eventType := "integration.unreachable"
		if to == integration.StatusConnected {
			severity, title = "success", inst.Name+" is back online"
			body = ""
			eventType = "integration.recovered"
		}
		notifySvc.NotifyResourceViewers(ctx, inst.ResourcePath(),
			func(ctx context.Context, userID uuid.UUID) bool {
				ok, err := engine.VisibleUnder(ctx, rbac.Principal{UserID: userID}, inst.ResourcePath())
				return err == nil && ok
			}, severity, title, body, eventType)
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
	if err := st.EnsureSchedule(ctx, "Metric rollups", "metrics.maintenance", map[string]any{}, "*/5 * * * *", next); err != nil {
		return err
	}
	next, err = jobs.NextRun("* * * * *", time.Now())
	if err != nil {
		return err
	}
	return st.EnsureSchedule(ctx, "Alert evaluation", "alerts.evaluate", map[string]any{}, "* * * * *", next)
}
