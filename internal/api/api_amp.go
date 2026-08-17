package api

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/TheOriUHD/homehub/internal/httpctx"
	"github.com/TheOriUHD/homehub/internal/integrations/amp"
	"github.com/TheOriUHD/homehub/internal/rbac"
	"github.com/TheOriUHD/homehub/internal/store"
)

// ampConn resolves an integration id to a live AMP connection, enforcing
// panel visibility.
func (s *Server) ampConn(ctx context.Context, id *httpctx.Identity, instID uuid.UUID) (*amp.Conn, string, error) {
	row, err := s.store.GetIntegration(ctx, instID)
	if err != nil || row.Type != amp.TypeID {
		return nil, "", store.ErrNotFound
	}
	resource := row.Type + "/" + instID.String()
	if !s.visibleUnder(ctx, id, resource) {
		return nil, "", store.ErrNotFound
	}
	conn, err := s.integrations.Connection(instID)
	if err != nil {
		return nil, resource, err
	}
	ac, ok := conn.(*amp.Conn)
	if !ok {
		return nil, resource, store.ErrNotFound
	}
	return ac, resource, nil
}

func (s *Server) AmpListInstances(ctx context.Context, _ AmpListInstancesRequestObject) (AmpListInstancesResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	rows, err := s.store.ListIntegrations(ctx)
	if err != nil {
		return nil, err
	}
	out := []AmpPanel{}
	for _, row := range rows {
		if row.Type != amp.TypeID || !row.Enabled {
			continue
		}
		resource := row.Type + "/" + row.ID.String()
		if !s.visibleUnder(ctx, id, resource) {
			continue
		}
		panel := AmpPanel{
			Id: row.ID, Name: row.Name,
			Status: AmpPanelStatus(row.Status), StatusMessage: row.StatusMessage,
		}
		if conn, cerr := s.integrations.Connection(row.ID); cerr == nil {
			if ac, ok := conn.(*amp.Conn); ok {
				ictx, cancel := context.WithTimeout(ctx, 10*time.Second)
				if targets, terr := ac.Targets(ictx); terr == nil {
					for _, t := range targets {
						for _, inst := range s.visibleAmpInstances(ctx, id, resource, t.Instances) {
							panel.InstanceCount++
							if inst.Running {
								panel.RunningCount++
							}
						}
					}
				}
				cancel()
			}
		}
		out = append(out, panel)
	}
	return AmpListInstances200JSONResponse(out), nil
}

// visibleAmpInstances filters game-server instances by per-instance view
// grants.
func (s *Server) visibleAmpInstances(ctx context.Context, id *httpctx.Identity, resource string, instances []amp.NormInstance) []amp.NormInstance {
	out := make([]amp.NormInstance, 0, len(instances))
	for _, inst := range instances {
		if s.can(ctx, id, resource+"/instance/"+inst.ID, rbac.LevelView) {
			out = append(out, inst)
		}
	}
	return out
}

func toAPIAmpInstance(inst amp.NormInstance) AmpServerInstance {
	out := AmpServerInstance{
		Id: inst.ID, Name: inst.Name, FriendlyName: inst.FriendlyName,
		Module: inst.Module, ModuleName: inst.ModuleName,
		Running: inst.Running, Suspended: inst.Suspended, State: inst.State,
	}
	if inst.Description != "" {
		out.Description = &inst.Description
	}
	if inst.IP != "" {
		out.Ip = &inst.IP
	}
	if inst.Port != 0 {
		port := inst.Port
		out.Port = &port
	}
	if inst.DiskUsageMB != 0 {
		disk := inst.DiskUsageMB
		out.DiskUsageMb = &disk
	}
	out.CpuPercent = inst.CPUPercent
	out.MemUsedMb = inst.MemUsedMB
	out.MemMaxMb = inst.MemMaxMB
	out.Players = inst.Players
	out.MaxPlayers = inst.MaxPlayers
	return out
}

func (s *Server) AmpOverview(ctx context.Context, req AmpOverviewRequestObject) (AmpOverviewResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, resource, err := s.ampConn(ctx, id, req.Id)
	if err == store.ErrNotFound {
		return AmpOverview404JSONResponse{notFoundErr("panel")}, nil
	}
	if err != nil {
		return AmpOverview502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	targets, err := conn.Targets(ctx)
	if err != nil {
		return AmpOverview502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	out := AmpOverview{Targets: []AmpTarget{}}
	for _, t := range targets {
		target := AmpTarget{
			InstanceId:   t.InstanceID,
			FriendlyName: t.FriendlyName,
			State:        t.State,
			Online:       t.Online,
			Instances:    []AmpServerInstance{},
		}
		if t.Description != "" {
			target.Description = &t.Description
		}
		if t.Platform != "" {
			target.Platform = &t.Platform
		}
		if t.CPUModel != "" {
			target.CpuModel = &t.CPUModel
		}
		if t.Cores != 0 {
			cores := t.Cores
			target.Cores = &cores
		}
		if t.RAMMB != 0 {
			ram := t.RAMMB
			target.RamMb = &ram
		}
		for _, inst := range s.visibleAmpInstances(ctx, id, resource, t.Instances) {
			target.Instances = append(target.Instances, toAPIAmpInstance(inst))
		}
		out.Targets = append(out.Targets, target)
	}
	return AmpOverview200JSONResponse(out), nil
}

// ampInstanceAccess authorizes one game-server instance at a level.
func (s *Server) ampInstanceAccess(ctx context.Context, id *httpctx.Identity, panelID uuid.UUID, instanceID string, level rbac.Level) (*amp.Conn, string, bool, error) {
	conn, resource, err := s.ampConn(ctx, id, panelID)
	if err != nil {
		return nil, resource, false, err
	}
	instResource := resource + "/instance/" + instanceID
	if !s.can(ctx, id, instResource, level) {
		return conn, instResource, false, nil
	}
	return conn, instResource, true, nil
}

func (s *Server) AmpInstanceStatus(ctx context.Context, req AmpInstanceStatusRequestObject) (AmpInstanceStatusResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, _, allowed, err := s.ampInstanceAccess(ctx, id, req.Id, req.InstanceId, rbac.LevelView)
	if err == store.ErrNotFound || (err == nil && !allowed) {
		return AmpInstanceStatus404JSONResponse{notFoundErr("instance")}, nil
	}
	if err != nil {
		return AmpInstanceStatus502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	status, err := conn.InstanceStatus(ctx, req.InstanceId)
	if err != nil {
		return AmpInstanceStatus502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	out := AmpInstanceStatus{State: amp.StateNames[status.State], Uptime: status.Uptime}
	if out.State == "" {
		out.State = "undefined"
	}
	if m, ok := status.Metrics["CPU Usage"]; ok {
		v := m.Percent
		out.CpuPercent = &v
	}
	if m, ok := status.Metrics["Memory Usage"]; ok {
		used, maxV := m.RawValue, m.MaxValue
		out.MemUsedMb, out.MemMaxMb = &used, &maxV
	}
	if m, ok := status.Metrics["Active Users"]; ok {
		players, maxP := int(m.RawValue), int(m.MaxValue)
		out.Players, out.MaxPlayers = &players, &maxP
	}
	return AmpInstanceStatus200JSONResponse(out), nil
}

func (s *Server) AmpConsole(ctx context.Context, req AmpConsoleRequestObject) (AmpConsoleResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, _, allowed, err := s.ampInstanceAccess(ctx, id, req.Id, req.InstanceId, rbac.LevelView)
	if err == store.ErrNotFound || (err == nil && !allowed) {
		return AmpConsole404JSONResponse{notFoundErr("instance")}, nil
	}
	if err != nil {
		return AmpConsole502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	after := int64(-1)
	if req.Params.After != nil {
		after = *req.Params.After
	}
	limit := 1000
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	lines, oldest, next, err := conn.Console(ctx, req.InstanceId, after, limit)
	if err != nil {
		return AmpConsole502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	out := AmpConsole{Lines: make([]AmpConsoleLine, 0, len(lines)), OldestSeq: oldest, NextSeq: next}
	for _, l := range lines {
		line := AmpConsoleLine{Seq: l.Seq, Ts: l.TSMillis, Type: l.Type, Contents: l.Contents}
		if l.Source != "" {
			src := l.Source
			line.Source = &src
		}
		out.Lines = append(out.Lines, line)
	}
	return AmpConsole200JSONResponse(out), nil
}

func (s *Server) AmpCommand(ctx context.Context, req AmpCommandRequestObject) (AmpCommandResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, resource, allowed, err := s.ampInstanceAccess(ctx, id, req.Id, req.InstanceId, rbac.LevelControl)
	if err == store.ErrNotFound {
		return AmpCommand404JSONResponse{notFoundErr("instance")}, nil
	}
	if err != nil {
		return AmpCommand502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	if !allowed {
		s.deniedAudit(ctx, id, "amp.command", resource)
		return AmpCommand403JSONResponse{forbidden()}, nil
	}
	command := strings.TrimSpace(req.Body.Command)
	if command == "" {
		return AmpCommand400JSONResponse{BadRequestJSONResponse(errBody("invalid_command", "command must not be empty"))}, nil
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := conn.SendCommand(cctx, req.InstanceId, command); err != nil {
		s.audit.Log(ctx, actorFor(id), "amp.command", resource, "error",
			map[string]any{"command": command, "error": err.Error()})
		return AmpCommand502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	s.audit.Log(ctx, actorFor(id), "amp.command", resource, "success", map[string]any{"command": command})
	return AmpCommand204Response{}, nil
}

func (s *Server) AmpAppAction(ctx context.Context, req AmpAppActionRequestObject) (AmpAppActionResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, resource, allowed, err := s.ampInstanceAccess(ctx, id, req.Id, req.InstanceId, rbac.LevelControl)
	if err == store.ErrNotFound {
		return AmpAppAction404JSONResponse{notFoundErr("instance")}, nil
	}
	if err != nil {
		return AmpAppAction502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	if !allowed {
		s.deniedAudit(ctx, id, "amp.app."+string(req.Body.Action), resource)
		return AmpAppAction403JSONResponse{forbidden()}, nil
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	action := string(req.Body.Action)
	if err := conn.AppAction(cctx, req.InstanceId, action); err != nil {
		s.audit.Log(ctx, actorFor(id), "amp.app."+action, resource, "error", map[string]any{"error": err.Error()})
		return AmpAppAction502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	s.audit.Log(ctx, actorFor(id), "amp.app."+action, resource, "success", nil)
	return AmpAppAction204Response{}, nil
}

func (s *Server) AmpInstanceAction(ctx context.Context, req AmpInstanceActionRequestObject) (AmpInstanceActionResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, resource, allowed, err := s.ampInstanceAccess(ctx, id, req.Id, req.InstanceId, rbac.LevelControl)
	if err == store.ErrNotFound {
		return AmpInstanceAction404JSONResponse{notFoundErr("instance")}, nil
	}
	if err != nil {
		return AmpInstanceAction502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	if !allowed {
		s.deniedAudit(ctx, id, "amp.instance."+string(req.Body.Action), resource)
		return AmpInstanceAction403JSONResponse{forbidden()}, nil
	}
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	action := string(req.Body.Action)
	if err := conn.InstanceAction(cctx, req.InstanceId, action); err != nil {
		s.audit.Log(ctx, actorFor(id), "amp.instance."+action, resource, "error", map[string]any{"error": err.Error()})
		return AmpInstanceAction502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	s.audit.Log(ctx, actorFor(id), "amp.instance."+action, resource, "success", nil)
	return AmpInstanceAction204Response{}, nil
}
