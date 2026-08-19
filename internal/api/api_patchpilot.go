package api

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/TheOriUHD/homehub/internal/httpctx"
	"github.com/TheOriUHD/homehub/internal/integrations/patchpilot"
	"github.com/TheOriUHD/homehub/internal/rbac"
	"github.com/TheOriUHD/homehub/internal/store"
)

// ppFleet resolves a fleet integration id, enforcing visibility. Returns the
// integration id and its RBAC resource path.
func (s *Server) ppFleet(ctx context.Context, id *httpctx.Identity, instID uuid.UUID) (uuid.UUID, string, error) {
	row, err := s.store.GetIntegration(ctx, instID)
	if err != nil || row.Type != patchpilot.TypeID {
		return uuid.Nil, "", store.ErrNotFound
	}
	resource := row.Type + "/" + instID.String()
	if !s.visibleUnder(ctx, id, resource) {
		return uuid.Nil, "", store.ErrNotFound
	}
	return instID, resource, nil
}

// ppHost resolves a host within a fleet, verifying membership and per-host
// visibility. Returns the host and its resource path.
func (s *Server) ppHost(ctx context.Context, id *httpctx.Identity, instID, hostID uuid.UUID) (*store.PatchpilotHost, string, error) {
	fleetID, root, err := s.ppFleet(ctx, id, instID)
	if err != nil {
		return nil, "", err
	}
	host, err := s.store.GetPatchpilotHost(ctx, hostID)
	if err != nil || host.IntegrationID != fleetID {
		return nil, "", store.ErrNotFound
	}
	resource := root + "/host/" + host.ID.String()
	if !s.visibleUnder(ctx, id, resource) {
		return nil, "", store.ErrNotFound
	}
	return host, resource, nil
}

// ---- mappers ----------------------------------------------------------------

func toAPIPatchpilotHost(h *store.PatchpilotHost) PatchpilotHost {
	return PatchpilotHost{
		Id:             h.ID,
		Name:           h.Name,
		Hostname:       h.Hostname,
		SshPort:        h.SSHPort,
		SshUser:        h.SSHUser,
		HostType:       h.HostType,
		PackageManager: h.PackageManager,
		OsPretty:       strPtr(h.OSPretty),
		Arch:           strPtr(h.Arch),
		IsProxmox:      h.IsProxmox,
		Enabled:        h.Enabled,
		AutoReboot:     h.AutoReboot,
		Notes:          strPtr(h.Notes),
		UpdatesPending: h.UpdatesPending,
		RebootRequired: h.RebootRequired,
		LastCheckedAt:  h.LastCheckedAt,
		LastRunStatus:  strPtr(h.LastRunStatus),
		LastRunAt:      h.LastRunAt,
	}
}

func toAPIPatchpilotRun(r *store.PatchpilotRun) PatchpilotRun {
	out := PatchpilotRun{
		Id:         r.ID,
		Trigger:    r.Trigger,
		Status:     r.Status,
		StartedAt:  r.StartedAt,
		FinishedAt: r.FinishedAt,
		Summary:    r.Summary,
	}
	if r.ScheduleID != nil {
		sid := openapi_types.UUID(*r.ScheduleID)
		out.ScheduleId = &sid
	}
	return out
}

func toAPIPatchpilotHostRun(hr *store.PatchpilotHostRun) PatchpilotHostRun {
	return PatchpilotHostRun{
		Id:               hr.ID,
		RunId:            hr.RunID,
		HostId:           hr.HostID,
		HostName:         hr.HostName,
		Status:           hr.Status,
		ExitCode:         hr.ExitCode,
		PackagesUpgraded: hr.PackagesUpgraded,
		RebootRequired:   hr.RebootRequired,
		ErrorSummary:     strPtr(hr.ErrorSummary),
		Log:              strPtr(hr.Log),
		StartedAt:        hr.StartedAt,
		FinishedAt:       hr.FinishedAt,
	}
}

func (s *Server) toAPIPatchpilotSchedule(ctx context.Context, sc *store.PatchpilotSchedule) PatchpilotSchedule {
	hostIDs, _ := s.store.PatchpilotScheduleHostIDs(ctx, sc.ID)
	ids := make([]openapi_types.UUID, 0, len(hostIDs))
	for _, h := range hostIDs {
		ids = append(ids, openapi_types.UUID(h))
	}
	return PatchpilotSchedule{
		Id:             sc.ID,
		Name:           sc.Name,
		CronExpr:       sc.CronExpr,
		Enabled:        sc.Enabled,
		SuppressReboot: sc.SuppressReboot,
		HostIds:        ids,
		LastRunAt:      sc.LastRunAt,
		NextRunAt:      sc.NextRunAt,
		LastSkippedAt:  sc.LastSkippedAt,
		LastSkipReason: strPtr(sc.LastSkipReason),
	}
}

// ---- hosts ------------------------------------------------------------------

func (s *Server) PatchpilotHosts(ctx context.Context, req PatchpilotHostsRequestObject) (PatchpilotHostsResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	fleetID, root, err := s.ppFleet(ctx, id, req.Id)
	if err != nil {
		return PatchpilotHosts404JSONResponse{notFoundErr("fleet")}, nil
	}
	hosts, err := s.store.ListPatchpilotHosts(ctx, fleetID)
	if err != nil {
		return nil, err
	}
	out := []PatchpilotHost{}
	for i := range hosts {
		if s.visibleUnder(ctx, id, root+"/host/"+hosts[i].ID.String()) {
			out = append(out, toAPIPatchpilotHost(&hosts[i]))
		}
	}
	return PatchpilotHosts200JSONResponse(out), nil
}

func (s *Server) PatchpilotHost(ctx context.Context, req PatchpilotHostRequestObject) (PatchpilotHostResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	host, _, err := s.ppHost(ctx, id, req.Id, uuid.UUID(req.HostId))
	if err != nil {
		return PatchpilotHost404JSONResponse{notFoundErr("host")}, nil
	}
	return PatchpilotHost200JSONResponse(toAPIPatchpilotHost(host)), nil
}

func (s *Server) PatchpilotDetect(ctx context.Context, req PatchpilotDetectRequestObject) (PatchpilotDetectResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	_, resource, err := s.ppFleet(ctx, id, req.Id)
	if err != nil {
		return PatchpilotDetect404JSONResponse{notFoundErr("fleet")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelConfigure) {
		return PatchpilotDetect404JSONResponse{notFoundErr("fleet")}, nil
	}
	b := req.Body
	cred := patchpilot.Credential{User: b.SshUser, Password: deref(b.Password)}
	res, err := s.patchpilot.Detect(ctx, b.Hostname, derefInt(b.SshPort, 22), cred, deref(b.RootPassword))
	if err != nil {
		return nil, err
	}
	return PatchpilotDetect200JSONResponse(toAPIDetect(res)), nil
}

func toAPIDetect(r *patchpilot.DetectResult) PatchpilotDetectResult {
	return PatchpilotDetectResult{
		Reachable:      r.Reachable,
		OsPretty:       strPtr(r.OSPretty),
		Arch:           strPtr(r.Arch),
		Hostname:       strPtr(r.Hostname),
		PackageManager: strPtr(r.PackageManager),
		IsProxmox:      boolPtr(r.IsProxmox),
		Escalation:     strPtr(r.Escalation),
		NeedsPassword:  boolPtr(r.NeedsPassword),
		Refusal:        strPtr(r.Refusal),
		Blocker:        strPtr(r.Blocker),
		Message:        r.Message,
	}
}

func (s *Server) PatchpilotOnboard(ctx context.Context, req PatchpilotOnboardRequestObject) (PatchpilotOnboardResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	fleetID, resource, err := s.ppFleet(ctx, id, req.Id)
	if err != nil {
		return PatchpilotOnboard404JSONResponse{notFoundErr("fleet")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "patchpilot.host.onboard", resource)
		return PatchpilotOnboard404JSONResponse{notFoundErr("fleet")}, nil
	}
	b := req.Body
	if strings.TrimSpace(b.Name) == "" || strings.TrimSpace(b.Hostname) == "" {
		return PatchpilotOnboard400JSONResponse{BadRequestJSONResponse(errBody("bad_input", "a name and hostname are required"))}, nil
	}
	in := patchpilot.CommitInput{
		Host:         b.Hostname,
		Port:         derefInt(b.SshPort, 22),
		Cred:         patchpilot.Credential{User: b.SshUser, Password: deref(b.Password)},
		RootPassword: deref(b.RootPassword),
		Name:         b.Name,
		Notes:        deref(b.Notes),
		AutoReboot:   derefBool(b.AutoReboot, true),
	}
	host, err := s.patchpilot.Onboard(ctx, fleetID, in, func(patchpilot.BootstrapStep) {})
	if err != nil {
		msg := err.Error()
		var be *patchpilot.BootstrapError
		if errors.As(err, &be) {
			msg = be.Summary + " — " + be.Guidance
		}
		return PatchpilotOnboard502JSONResponse{UpstreamErrorJSONResponse(errBody("bootstrap_failed", msg))}, nil
	}
	s.audit.Log(ctx, actorFor(id), "patchpilot.host.onboard", resource, "success", map[string]any{"host": host.Name})
	s.integrations.Kick(fleetID)
	return PatchpilotOnboard201JSONResponse(toAPIPatchpilotHost(host)), nil
}

func (s *Server) PatchpilotUpdateHost(ctx context.Context, req PatchpilotUpdateHostRequestObject) (PatchpilotUpdateHostResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	host, resource, err := s.ppHost(ctx, id, req.Id, uuid.UUID(req.HostId))
	if err != nil {
		return PatchpilotUpdateHost404JSONResponse{notFoundErr("host")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelConfigure) {
		return PatchpilotUpdateHost404JSONResponse{notFoundErr("host")}, nil
	}
	b := req.Body
	name := host.Name
	if b.Name != nil && strings.TrimSpace(*b.Name) != "" {
		name = *b.Name
	}
	updated, err := s.store.UpdatePatchpilotHost(ctx, host.ID, name,
		derefBool(b.Enabled, host.Enabled), derefBool(b.AutoReboot, host.AutoReboot), derefStr(b.Notes, host.Notes))
	if err != nil {
		return nil, err
	}
	return PatchpilotUpdateHost200JSONResponse(toAPIPatchpilotHost(updated)), nil
}

func (s *Server) PatchpilotDeleteHost(ctx context.Context, req PatchpilotDeleteHostRequestObject) (PatchpilotDeleteHostResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	host, resource, err := s.ppHost(ctx, id, req.Id, uuid.UUID(req.HostId))
	if err != nil {
		return PatchpilotDeleteHost404JSONResponse{notFoundErr("host")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelConfigure) {
		return PatchpilotDeleteHost404JSONResponse{notFoundErr("host")}, nil
	}
	if err := s.store.DeletePatchpilotHost(ctx, host.ID); err != nil {
		return nil, err
	}
	s.audit.Log(ctx, actorFor(id), "patchpilot.host.delete", resource, "success", map[string]any{"host": host.Name})
	return PatchpilotDeleteHost204Response{}, nil
}

func (s *Server) PatchpilotDryRun(ctx context.Context, req PatchpilotDryRunRequestObject) (PatchpilotDryRunResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	host, resource, err := s.ppHost(ctx, id, req.Id, uuid.UUID(req.HostId))
	if err != nil {
		return PatchpilotDryRun404JSONResponse{notFoundErr("host")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelView) {
		return PatchpilotDryRun404JSONResponse{notFoundErr("host")}, nil
	}
	res, err := s.patchpilot.DryRun(ctx, host)
	if err != nil {
		return PatchpilotDryRun502JSONResponse{UpstreamErrorJSONResponse(errBody("dry_run_failed", err.Error()))}, nil
	}
	return PatchpilotDryRun200JSONResponse(PatchpilotDryRunResult{
		PackagesUpgraded: res.PackagesUpgraded,
		PackagesRemoved:  res.PackagesRemoved,
		RebootRequired:   res.RebootRequired,
		Summary:          res.Summary,
		RawOutput:        res.RawOutput,
	}), nil
}

func (s *Server) PatchpilotRunHost(ctx context.Context, req PatchpilotRunHostRequestObject) (PatchpilotRunHostResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	host, resource, err := s.ppHost(ctx, id, req.Id, uuid.UUID(req.HostId))
	if err != nil {
		return PatchpilotRunHost404JSONResponse{notFoundErr("host")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelControl) {
		s.deniedAudit(ctx, id, "patchpilot.host.run", resource)
		return PatchpilotRunHost404JSONResponse{notFoundErr("host")}, nil
	}
	run, err := s.patchpilot.StartRun(ctx, host.IntegrationID, "manual", nil, []uuid.UUID{host.ID}, false)
	if err != nil {
		if errors.As(err, &patchpilot.ErrRunInProgress{}) {
			return PatchpilotRunHost409JSONResponse{ConflictJSONResponse(errBody("run_in_progress", err.Error()))}, nil
		}
		return nil, err
	}
	s.audit.Log(ctx, actorFor(id), "patchpilot.host.run", resource, "success", map[string]any{"host": host.Name})
	return PatchpilotRunHost202JSONResponse(toAPIPatchpilotRun(run)), nil
}

func (s *Server) PatchpilotRebootHost(ctx context.Context, req PatchpilotRebootHostRequestObject) (PatchpilotRebootHostResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	host, resource, err := s.ppHost(ctx, id, req.Id, uuid.UUID(req.HostId))
	if err != nil {
		return PatchpilotRebootHost404JSONResponse{notFoundErr("host")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelControl) {
		s.deniedAudit(ctx, id, "patchpilot.host.reboot", resource)
		return PatchpilotRebootHost404JSONResponse{notFoundErr("host")}, nil
	}
	if err := s.patchpilot.Reboot(ctx, host); err != nil {
		return PatchpilotRebootHost502JSONResponse{UpstreamErrorJSONResponse(errBody("reboot_failed", err.Error()))}, nil
	}
	s.audit.Log(ctx, actorFor(id), "patchpilot.host.reboot", resource, "success", nil)
	return PatchpilotRebootHost200Response{}, nil
}

func (s *Server) PatchpilotRepairSudoers(ctx context.Context, req PatchpilotRepairSudoersRequestObject) (PatchpilotRepairSudoersResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	host, resource, err := s.ppHost(ctx, id, req.Id, uuid.UUID(req.HostId))
	if err != nil {
		return PatchpilotRepairSudoers404JSONResponse{notFoundErr("host")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelConfigure) {
		return PatchpilotRepairSudoers404JSONResponse{notFoundErr("host")}, nil
	}
	if err := s.patchpilot.RepairSudoers(ctx, host); err != nil {
		return PatchpilotRepairSudoers502JSONResponse{UpstreamErrorJSONResponse(errBody("repair_failed", err.Error()))}, nil
	}
	return PatchpilotRepairSudoers200Response{}, nil
}

func (s *Server) PatchpilotHostRuns(ctx context.Context, req PatchpilotHostRunsRequestObject) (PatchpilotHostRunsResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	host, _, err := s.ppHost(ctx, id, req.Id, uuid.UUID(req.HostId))
	if err != nil {
		return PatchpilotHostRuns404JSONResponse{notFoundErr("host")}, nil
	}
	runs, err := s.store.RecentPatchpilotHostRuns(ctx, host.ID, 30)
	if err != nil {
		return nil, err
	}
	out := []PatchpilotHostRun{}
	for i := range runs {
		out = append(out, toAPIPatchpilotHostRun(&runs[i]))
	}
	return PatchpilotHostRuns200JSONResponse(out), nil
}

func (s *Server) PatchpilotPublicKey(ctx context.Context, req PatchpilotPublicKeyRequestObject) (PatchpilotPublicKeyResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	fleetID, resource, err := s.ppFleet(ctx, id, req.Id)
	if err != nil {
		return PatchpilotPublicKey404JSONResponse{notFoundErr("fleet")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelConfigure) {
		return PatchpilotPublicKey404JSONResponse{notFoundErr("fleet")}, nil
	}
	pub, err := s.patchpilot.PublicKey(ctx, fleetID)
	if err != nil {
		return nil, err
	}
	return PatchpilotPublicKey200JSONResponse{PublicKey: strings.TrimSpace(pub)}, nil
}

// ---- runs -------------------------------------------------------------------

func (s *Server) PatchpilotRun(ctx context.Context, req PatchpilotRunRequestObject) (PatchpilotRunResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	fleetID, resource, err := s.ppFleet(ctx, id, req.Id)
	if err != nil {
		return PatchpilotRun404JSONResponse{notFoundErr("fleet")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelControl) {
		s.deniedAudit(ctx, id, "patchpilot.run", resource)
		return PatchpilotRun404JSONResponse{notFoundErr("fleet")}, nil
	}
	var hostIDs []uuid.UUID
	suppress := false
	if req.Body != nil {
		if req.Body.HostIds != nil {
			for _, h := range *req.Body.HostIds {
				hostIDs = append(hostIDs, uuid.UUID(h))
			}
		}
		suppress = derefBool(req.Body.SuppressReboot, false)
	}
	run, err := s.patchpilot.StartRun(ctx, fleetID, "manual", nil, hostIDs, suppress)
	if err != nil {
		if errors.As(err, &patchpilot.ErrRunInProgress{}) {
			return PatchpilotRun409JSONResponse{ConflictJSONResponse(errBody("run_in_progress", err.Error()))}, nil
		}
		return nil, err
	}
	s.audit.Log(ctx, actorFor(id), "patchpilot.run", resource, "success", nil)
	return PatchpilotRun202JSONResponse(toAPIPatchpilotRun(run)), nil
}

func (s *Server) PatchpilotRuns(ctx context.Context, req PatchpilotRunsRequestObject) (PatchpilotRunsResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	fleetID, _, err := s.ppFleet(ctx, id, req.Id)
	if err != nil {
		return PatchpilotRuns404JSONResponse{notFoundErr("fleet")}, nil
	}
	limit := 100
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	runs, err := s.store.ListPatchpilotRuns(ctx, fleetID, limit)
	if err != nil {
		return nil, err
	}
	out := []PatchpilotRun{}
	for i := range runs {
		out = append(out, toAPIPatchpilotRun(&runs[i]))
	}
	return PatchpilotRuns200JSONResponse(out), nil
}

func (s *Server) PatchpilotRunDetail(ctx context.Context, req PatchpilotRunDetailRequestObject) (PatchpilotRunDetailResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	fleetID, root, err := s.ppFleet(ctx, id, req.Id)
	if err != nil {
		return PatchpilotRunDetail404JSONResponse{notFoundErr("fleet")}, nil
	}
	run, err := s.store.GetPatchpilotRun(ctx, uuid.UUID(req.RunId))
	if err != nil || run.IntegrationID != fleetID {
		return PatchpilotRunDetail404JSONResponse{notFoundErr("run")}, nil
	}
	hostRuns, err := s.store.ListPatchpilotHostRuns(ctx, run.ID)
	if err != nil {
		return nil, err
	}
	// A run spans the whole fleet, but each host run carries that host's full
	// command log, package inventory and hostname. Return only the host runs
	// the caller may see — the same per-host visibility PatchpilotHosts applies
	// — so a member granted view on one host can't read sibling hosts' output.
	// Orphaned rows (host deleted → nil HostID) show only to fleet-level viewers.
	fleetViewer := s.can(ctx, id, root, rbac.LevelView)
	hrs := []PatchpilotHostRun{}
	for i := range hostRuns {
		visible := fleetViewer
		if hr := hostRuns[i].HostID; hr != nil {
			visible = s.visibleUnder(ctx, id, root+"/host/"+hr.String())
		}
		if visible {
			hrs = append(hrs, toAPIPatchpilotHostRun(&hostRuns[i]))
		}
	}
	return PatchpilotRunDetail200JSONResponse(PatchpilotRunDetail{
		Run:      toAPIPatchpilotRun(run),
		HostRuns: hrs,
	}), nil
}

// ---- schedules --------------------------------------------------------------

func (s *Server) PatchpilotSchedules(ctx context.Context, req PatchpilotSchedulesRequestObject) (PatchpilotSchedulesResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	fleetID, _, err := s.ppFleet(ctx, id, req.Id)
	if err != nil {
		return PatchpilotSchedules404JSONResponse{notFoundErr("fleet")}, nil
	}
	scheds, err := s.store.ListPatchpilotSchedules(ctx, fleetID)
	if err != nil {
		return nil, err
	}
	out := []PatchpilotSchedule{}
	for i := range scheds {
		out = append(out, s.toAPIPatchpilotSchedule(ctx, &scheds[i]))
	}
	return PatchpilotSchedules200JSONResponse(out), nil
}

func (s *Server) PatchpilotCreateSchedule(ctx context.Context, req PatchpilotCreateScheduleRequestObject) (PatchpilotCreateScheduleResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	fleetID, resource, err := s.ppFleet(ctx, id, req.Id)
	if err != nil {
		return PatchpilotCreateSchedule404JSONResponse{notFoundErr("fleet")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelConfigure) {
		return PatchpilotCreateSchedule404JSONResponse{notFoundErr("fleet")}, nil
	}
	b := req.Body
	next, verr := patchpilot.NextRun(b.CronExpr)
	if verr != nil {
		return PatchpilotCreateSchedule400JSONResponse{BadRequestJSONResponse(errBody("bad_cron", verr.Error()))}, nil
	}
	sc := store.PatchpilotSchedule{
		IntegrationID:  fleetID,
		Name:           b.Name,
		CronExpr:       b.CronExpr,
		Enabled:        derefBool(b.Enabled, true),
		SuppressReboot: derefBool(b.SuppressReboot, false),
		NextRunAt:      &next,
	}
	row, err := s.store.CreatePatchpilotSchedule(ctx, sc, uuidList(b.HostIds))
	if err != nil {
		return nil, err
	}
	out := s.toAPIPatchpilotSchedule(ctx, row)
	return PatchpilotCreateSchedule201JSONResponse(out), nil
}

func (s *Server) PatchpilotUpdateSchedule(ctx context.Context, req PatchpilotUpdateScheduleRequestObject) (PatchpilotUpdateScheduleResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	fleetID, resource, err := s.ppFleet(ctx, id, req.Id)
	if err != nil {
		return PatchpilotUpdateSchedule404JSONResponse{notFoundErr("fleet")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelConfigure) {
		return PatchpilotUpdateSchedule404JSONResponse{notFoundErr("fleet")}, nil
	}
	existing, err := s.store.GetPatchpilotSchedule(ctx, uuid.UUID(req.ScheduleId))
	if err != nil || existing.IntegrationID != fleetID {
		return PatchpilotUpdateSchedule404JSONResponse{notFoundErr("schedule")}, nil
	}
	b := req.Body
	next, verr := patchpilot.NextRun(b.CronExpr)
	if verr != nil {
		return PatchpilotUpdateSchedule400JSONResponse{BadRequestJSONResponse(errBody("bad_cron", verr.Error()))}, nil
	}
	sc := store.PatchpilotSchedule{
		Name:           b.Name,
		CronExpr:       b.CronExpr,
		Enabled:        derefBool(b.Enabled, true),
		SuppressReboot: derefBool(b.SuppressReboot, false),
		NextRunAt:      &next,
	}
	row, err := s.store.UpdatePatchpilotSchedule(ctx, existing.ID, sc, uuidList(b.HostIds))
	if err != nil {
		return nil, err
	}
	return PatchpilotUpdateSchedule200JSONResponse(s.toAPIPatchpilotSchedule(ctx, row)), nil
}

func (s *Server) PatchpilotDeleteSchedule(ctx context.Context, req PatchpilotDeleteScheduleRequestObject) (PatchpilotDeleteScheduleResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	fleetID, resource, err := s.ppFleet(ctx, id, req.Id)
	if err != nil {
		return PatchpilotDeleteSchedule404JSONResponse{notFoundErr("fleet")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelConfigure) {
		return PatchpilotDeleteSchedule404JSONResponse{notFoundErr("fleet")}, nil
	}
	existing, err := s.store.GetPatchpilotSchedule(ctx, uuid.UUID(req.ScheduleId))
	if err != nil || existing.IntegrationID != fleetID {
		return PatchpilotDeleteSchedule404JSONResponse{notFoundErr("schedule")}, nil
	}
	if err := s.store.DeletePatchpilotSchedule(ctx, existing.ID); err != nil {
		return nil, err
	}
	return PatchpilotDeleteSchedule204Response{}, nil
}

func (s *Server) PatchpilotRunSchedule(ctx context.Context, req PatchpilotRunScheduleRequestObject) (PatchpilotRunScheduleResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	fleetID, resource, err := s.ppFleet(ctx, id, req.Id)
	if err != nil {
		return PatchpilotRunSchedule404JSONResponse{notFoundErr("fleet")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelControl) {
		s.deniedAudit(ctx, id, "patchpilot.schedule.run", resource)
		return PatchpilotRunSchedule404JSONResponse{notFoundErr("fleet")}, nil
	}
	sched, err := s.store.GetPatchpilotSchedule(ctx, uuid.UUID(req.ScheduleId))
	if err != nil || sched.IntegrationID != fleetID {
		return PatchpilotRunSchedule404JSONResponse{notFoundErr("schedule")}, nil
	}
	hostIDs, _ := s.store.PatchpilotScheduleHostIDs(ctx, sched.ID)
	run, err := s.patchpilot.StartRun(ctx, fleetID, "manual", &sched.ID, hostIDs, sched.SuppressReboot)
	if err != nil {
		if errors.As(err, &patchpilot.ErrRunInProgress{}) {
			return PatchpilotRunSchedule409JSONResponse{ConflictJSONResponse(errBody("run_in_progress", err.Error()))}, nil
		}
		return nil, err
	}
	return PatchpilotRunSchedule202JSONResponse(toAPIPatchpilotRun(run)), nil
}

// ---- small helpers ----------------------------------------------------------

func derefStr(p *string, def string) string {
	if p == nil {
		return def
	}
	return *p
}
func derefInt(p *int, def int) int {
	if p == nil || *p == 0 {
		return def
	}
	return *p
}
func derefBool(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}
func boolPtr(b bool) *bool { return &b }

func uuidList(in []openapi_types.UUID) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(in))
	for _, u := range in {
		out = append(out, uuid.UUID(u))
	}
	return out
}
