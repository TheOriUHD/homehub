package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/TheOriUHD/homehub/internal/integrations/patchpilot"
	"github.com/TheOriUHD/homehub/internal/jobs"
	"github.com/TheOriUHD/homehub/internal/store"
)

// registerPatchpilotJobs wires the every-minute scan that turns due PatchPilot
// schedules into update runs. The generic scheduler fires "patchpilot.check"
// once a minute; the handler evaluates each fleet's own schedule table (which
// carries host ordering and overlap policy the generic scheduler doesn't).
func registerPatchpilotJobs(jobEngine *jobs.Engine, st *store.Store, reg *patchpilot.Registry, logger *slog.Logger) {
	log := logger.With("component", "patchpilot-scheduler")
	jobEngine.Register("patchpilot.check", func(ctx context.Context, _ json.RawMessage) error {
		now := time.Now()
		due, err := st.DuePatchpilotSchedules(ctx, now)
		if err != nil {
			return err
		}
		for _, sched := range due {
			// Advance next_run_at first so a slow run can't re-fire the same
			// schedule on the following tick.
			next, nerr := patchpilot.NextRun(sched.CronExpr)
			if nerr != nil {
				log.Error("invalid cron on patchpilot schedule", "schedule", sched.Name, "error", nerr)
				continue
			}
			_ = st.MarkPatchpilotScheduleRun(ctx, sched.ID, now, next)

			hostIDs, herr := st.PatchpilotScheduleHostIDs(ctx, sched.ID)
			if herr != nil || len(hostIDs) == 0 {
				continue
			}
			schedID := sched.ID
			if _, rerr := reg.StartRun(ctx, sched.IntegrationID, "scheduled", &schedID, hostIDs, sched.SuppressReboot); rerr != nil {
				// Overlap skips are recorded on the schedule inside StartRun.
				log.Info("scheduled run not started", "schedule", sched.Name, "reason", rerr.Error())
			}
		}
		return nil
	})
}
