package api

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"homehub/internal/httpctx"
	"homehub/internal/integrations/patchpilot"
	"homehub/internal/rbac"
	"homehub/internal/store"
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

// apiHost maps a host row for the API. A bound host shows the registry
// entry's current address and name, so what the operator sees is what will
// be dialed — not the snapshot from onboarding day.
func (s *Server) apiHost(ctx context.Context, h *store.PatchpilotHost) PatchpilotHost {
	out := PatchpilotHost{
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
		Critical:       h.Critical,
		Enabled:        h.Enabled,
		AutoReboot:     h.AutoReboot,
		Notes:          strPtr(h.Notes),
		UpdatesPending: h.UpdatesPending,
		RebootRequired: h.RebootRequired,
		LastCheckedAt:  h.LastCheckedAt,
		LastRunStatus:  strPtr(h.LastRunStatus),
		LastRunAt:      h.LastRunAt,
	}
	if h.SSHHostID != nil {
		sid := openapi_types.UUID(*h.SSHHostID)
		out.SshHostId = &sid
		if sh, err := s.store.GetSSHHost(ctx, *h.SSHHostID); err == nil {
			out.Hostname, out.SshPort = sh.Hostname, sh.Port
			out.SshHostName = strPtr(sh.Name)
		}
	}
	return out
}

// ppLogin is what onboarding dials: a registry entry, or the fields typed by
// hand. sshHostID and sshHostName carry the binding into the created row.
type ppLogin struct {
	host        string
	port        int
	cred        patchpilot.Credential
	sshHostID   *uuid.UUID
	sshHostName string
}

var (
	errSSHHostHidden = errors.New("SSH host not found")
	errPPBadLogin    = errors.New("pick a registry host, or enter a hostname and SSH username")
)

// ppLoginFromRegistry builds the login from a registry entry the caller may
// use — control on that host, the same as opening a terminal on it, since
// the bootstrap runs commands as that login. A stored key logs in and the
// password typed in the wizard, if any, is the sudo password; a stored
// password serves both purposes, as a typed one does on a manual host.
func (s *Server) ppLoginFromRegistry(ctx context.Context, id *httpctx.Identity, hostID uuid.UUID, typedPassword string) (*ppLogin, error) {
	sh, err := s.store.GetSSHHost(ctx, hostID)
	if err != nil {
		return nil, errSSHHostHidden
	}
	if !s.can(ctx, id, ResSSHHosts, rbac.LevelControl) && !s.can(ctx, id, sshHostResource(hostID), rbac.LevelControl) {
		return nil, errSSHHostHidden
	}
	secret, err := s.vault.Open(ctx, sshVaultScope, hostID.String())
	if err != nil {
		return nil, errors.New("the registry entry's stored secret is unreadable — re-enter its credentials")
	}
	cred := patchpilot.Credential{User: sh.Username, Password: typedPassword}
	if sh.AuthType == "key" {
		cred.Key = string(secret)
	} else {
		cred.Password = string(secret)
	}
	return &ppLogin{host: sh.Hostname, port: sh.Port, cred: cred, sshHostID: &sh.ID, sshHostName: sh.Name}, nil
}

// ppLoginFrom resolves the wizard's credential block either way.
func (s *Server) ppLoginFrom(ctx context.Context, id *httpctx.Identity, sshHostID *openapi_types.UUID, hostname, sshUser *string, sshPort *int, password string) (*ppLogin, error) {
	if sshHostID != nil {
		return s.ppLoginFromRegistry(ctx, id, uuid.UUID(*sshHostID), password)
	}
	host, user := strings.TrimSpace(deref(hostname)), strings.TrimSpace(deref(sshUser))
	if host == "" || user == "" {
		return nil, errPPBadLogin
	}
	return &ppLogin{host: host, port: derefInt(sshPort, 22), cred: patchpilot.Credential{User: user, Password: password}}, nil
}

// bootstrapMessage is what the operator reads when a bootstrap fails: the
// summary, and the guidance only when it adds something the summary did not
// already say.
func bootstrapMessage(err error) string {
	var be *patchpilot.BootstrapError
	if !errors.As(err, &be) {
		return err.Error()
	}
	if be.Guidance == "" || strings.Contains(be.Summary, be.Guidance) {
		return be.Summary
	}
	return be.Summary + " — " + be.Guidance
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
			out = append(out, s.apiHost(ctx, &hosts[i]))
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
	return PatchpilotHost200JSONResponse(s.apiHost(ctx, host)), nil
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
	login, lerr := s.ppLoginFrom(ctx, id, b.SshHostId, b.Hostname, b.SshUser, b.SshPort, deref(b.Password))
	switch {
	case errors.Is(lerr, errSSHHostHidden):
		return PatchpilotDetect404JSONResponse{notFoundErr("SSH host")}, nil
	case lerr != nil:
		return PatchpilotDetect400JSONResponse{BadRequestJSONResponse(errBody("bad_input", lerr.Error()))}, nil
	}
	res, err := s.patchpilot.Detect(ctx, login.host, login.port, login.cred, deref(b.RootPassword))
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
	login, lerr := s.ppLoginFrom(ctx, id, b.SshHostId, b.Hostname, b.SshUser, b.SshPort, deref(b.Password))
	switch {
	case errors.Is(lerr, errSSHHostHidden):
		return PatchpilotOnboard404JSONResponse{notFoundErr("SSH host")}, nil
	case lerr != nil:
		return PatchpilotOnboard400JSONResponse{BadRequestJSONResponse(errBody("bad_input", lerr.Error()))}, nil
	}
	// A bound host is named after its registry entry unless the wizard says
	// otherwise; a manual one has to be named.
	name := strings.TrimSpace(deref(b.Name))
	if name == "" {
		name = login.sshHostName
	}
	if name == "" {
		return PatchpilotOnboard400JSONResponse{BadRequestJSONResponse(errBody("bad_input", "a name is required"))}, nil
	}
	in := patchpilot.CommitInput{
		Host:         login.host,
		Port:         login.port,
		Cred:         login.cred,
		RootPassword: deref(b.RootPassword),
		Name:         name,
		Notes:        deref(b.Notes),
		AutoReboot:   derefBool(b.AutoReboot, true),
		SSHHostID:    login.sshHostID,
	}
	host, err := s.patchpilot.Onboard(ctx, fleetID, in, func(patchpilot.BootstrapStep) {})
	if err != nil {
		msg := bootstrapMessage(err)
		return PatchpilotOnboard502JSONResponse{UpstreamErrorJSONResponse(errBody("bootstrap_failed", msg))}, nil
	}
	s.audit.Log(ctx, actorFor(id), "patchpilot.host.onboard", resource, "success", map[string]any{"host": host.Name})
	s.integrations.Kick(fleetID)
	return PatchpilotOnboard201JSONResponse(s.apiHost(ctx, host)), nil
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
		name = strings.TrimSpace(*b.Name)
	}
	// The address is editable on a manual host only; a bound one follows its
	// registry entry, and silently accepting a new address here would just
	// be overwritten on the next connection.
	if b.Hostname != nil || b.SshPort != nil {
		if host.SSHHostID != nil {
			return PatchpilotUpdateHost400JSONResponse{BadRequestJSONResponse(errBody("address_bound",
				"this host takes its address from its registry entry — edit the entry, or unbind the host first"))}, nil
		}
		hostname := strings.TrimSpace(derefStr(b.Hostname, host.Hostname))
		port := derefInt(b.SshPort, host.SSHPort)
		if hostname == "" || port < 1 || port > 65535 {
			return PatchpilotUpdateHost400JSONResponse{BadRequestJSONResponse(errBody("bad_input", "a hostname and a port between 1 and 65535 are required"))}, nil
		}
		if err := s.store.SetPatchpilotHostEndpoint(ctx, host.ID, hostname, port); err != nil {
			return nil, err
		}
	}
	updated, err := s.store.UpdatePatchpilotHost(ctx, host.ID, name,
		derefBool(b.Enabled, host.Enabled), derefBool(b.AutoReboot, host.AutoReboot),
		derefBool(b.Critical, host.Critical), derefStr(b.Notes, host.Notes))
	if err != nil {
		return nil, err
	}
	s.audit.Log(ctx, actorFor(id), "patchpilot.host.update", resource, "success", map[string]any{"host": updated.Name})
	return PatchpilotUpdateHost200JSONResponse(s.apiHost(ctx, updated)), nil
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

// PatchpilotBindHost points a host at a registry entry, or unbinds it. The
// address is pulled in right away so the row never shows a stale one.
func (s *Server) PatchpilotBindHost(ctx context.Context, req PatchpilotBindHostRequestObject) (PatchpilotBindHostResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	host, resource, err := s.ppHost(ctx, id, req.Id, uuid.UUID(req.HostId))
	if err != nil {
		return PatchpilotBindHost404JSONResponse{notFoundErr("host")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelConfigure) {
		return PatchpilotBindHost404JSONResponse{notFoundErr("host")}, nil
	}
	var target *uuid.UUID
	if req.Body.SshHostId != nil {
		sid := uuid.UUID(*req.Body.SshHostId)
		if _, err := s.store.GetSSHHost(ctx, sid); err != nil {
			return PatchpilotBindHost404JSONResponse{notFoundErr("SSH host")}, nil
		}
		// Binding lets the fleet dial this entry and, on a re-run, log in as
		// it — the same permission as opening a terminal on it.
		if !s.can(ctx, id, ResSSHHosts, rbac.LevelControl) && !s.can(ctx, id, sshHostResource(sid), rbac.LevelControl) {
			return PatchpilotBindHost404JSONResponse{notFoundErr("SSH host")}, nil
		}
		target = &sid
	}
	updated, err := s.store.SetPatchpilotHostBinding(ctx, host.ID, target)
	if err != nil {
		return nil, err
	}
	s.patchpilot.Endpoint(ctx, updated)
	details := map[string]any{"host": host.Name, "sshHostId": nil}
	if target != nil {
		details["sshHostId"] = target.String()
	}
	s.audit.Log(ctx, actorFor(id), "patchpilot.host.bind", resource, "success", details)
	return PatchpilotBindHost200JSONResponse(s.apiHost(ctx, updated)), nil
}

// PatchpilotRebootstrapHost runs the bootstrap again on a known host: with
// the bound registry entry's credential, or with a login typed for the
// occasion on a manual host.
func (s *Server) PatchpilotRebootstrapHost(ctx context.Context, req PatchpilotRebootstrapHostRequestObject) (PatchpilotRebootstrapHostResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	host, resource, err := s.ppHost(ctx, id, req.Id, uuid.UUID(req.HostId))
	if err != nil {
		return PatchpilotRebootstrapHost404JSONResponse{notFoundErr("host")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "patchpilot.host.rebootstrap", resource)
		return PatchpilotRebootstrapHost404JSONResponse{notFoundErr("host")}, nil
	}
	b := req.Body
	var login *ppLogin
	if host.SSHHostID != nil {
		l, lerr := s.ppLoginFromRegistry(ctx, id, *host.SSHHostID, deref(b.Password))
		switch {
		case errors.Is(lerr, errSSHHostHidden):
			return PatchpilotRebootstrapHost404JSONResponse{notFoundErr("SSH host")}, nil
		case lerr != nil:
			return PatchpilotRebootstrapHost400JSONResponse{BadRequestJSONResponse(errBody("bad_input", lerr.Error()))}, nil
		}
		login = l
	} else {
		user := strings.TrimSpace(deref(b.SshUser))
		if user == "" || deref(b.Password) == "" {
			return PatchpilotRebootstrapHost400JSONResponse{BadRequestJSONResponse(errBody("bad_input",
				"a login user and password are needed for a host that is not bound to the registry"))}, nil
		}
		login = &ppLogin{host: host.Hostname, port: host.SSHPort, cred: patchpilot.Credential{User: user, Password: deref(b.Password)}}
	}
	if err := s.patchpilot.Rebootstrap(ctx, host, login.host, login.port, login.cred, deref(b.RootPassword), func(patchpilot.BootstrapStep) {}); err != nil {
		msg := bootstrapMessage(err)
		return PatchpilotRebootstrapHost502JSONResponse{UpstreamErrorJSONResponse(errBody("bootstrap_failed", msg))}, nil
	}
	s.audit.Log(ctx, actorFor(id), "patchpilot.host.rebootstrap", resource, "success", map[string]any{"host": host.Name})
	s.integrations.Kick(host.IntegrationID)
	fresh, err := s.store.GetPatchpilotHost(ctx, host.ID)
	if err != nil {
		return nil, err
	}
	return PatchpilotRebootstrapHost200JSONResponse(s.apiHost(ctx, fresh)), nil
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

func (s *Server) PatchpilotAbortRun(ctx context.Context, req PatchpilotAbortRunRequestObject) (PatchpilotAbortRunResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	fleetID, resource, err := s.ppFleet(ctx, id, req.Id)
	if err != nil {
		return PatchpilotAbortRun404JSONResponse{notFoundErr("fleet")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelControl) {
		s.deniedAudit(ctx, id, "patchpilot.abort", resource)
		return PatchpilotAbortRun403JSONResponse{forbidden()}, nil
	}
	run, err := s.store.GetPatchpilotRun(ctx, uuid.UUID(req.RunId))
	if err != nil || run.IntegrationID != fleetID {
		return PatchpilotAbortRun404JSONResponse{notFoundErr("run")}, nil
	}
	if aerr := s.patchpilot.AbortRun(uuid.UUID(req.RunId)); aerr != nil {
		return PatchpilotAbortRun404JSONResponse{notFoundErr("run")}, nil // not executing here
	}
	s.audit.Log(ctx, actorFor(id), "patchpilot.abort", resource, "success", map[string]any{"runId": run.ID})
	return PatchpilotAbortRun202Response{}, nil
}

// PatchpilotSkipHost takes one host out of a run that is executing here,
// with the same permission an abort needs.
func (s *Server) PatchpilotSkipHost(ctx context.Context, req PatchpilotSkipHostRequestObject) (PatchpilotSkipHostResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	fleetID, resource, err := s.ppFleet(ctx, id, req.Id)
	if err != nil {
		return PatchpilotSkipHost404JSONResponse{notFoundErr("fleet")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelControl) {
		s.deniedAudit(ctx, id, "patchpilot.host.skip", resource)
		return PatchpilotSkipHost403JSONResponse{forbidden()}, nil
	}
	run, err := s.store.GetPatchpilotRun(ctx, uuid.UUID(req.RunId))
	if err != nil || run.IntegrationID != fleetID {
		return PatchpilotSkipHost404JSONResponse{notFoundErr("run")}, nil
	}
	host, _, err := s.ppHost(ctx, id, req.Id, uuid.UUID(req.HostId))
	if err != nil {
		return PatchpilotSkipHost404JSONResponse{notFoundErr("host")}, nil
	}
	if serr := s.patchpilot.SkipHost(run.ID, host.ID); serr != nil {
		return PatchpilotSkipHost404JSONResponse{notFoundErr("run")}, nil // not executing here
	}
	s.audit.Log(ctx, actorFor(id), "patchpilot.host.skip", resource, "success", map[string]any{"runId": run.ID, "host": host.Name})
	return PatchpilotSkipHost202Response{}, nil
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
