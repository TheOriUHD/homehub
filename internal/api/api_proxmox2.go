package api

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/TheOriUHD/homehub/internal/httpctx"
	"github.com/TheOriUHD/homehub/internal/pve"
	"github.com/TheOriUHD/homehub/internal/rbac"
	"github.com/TheOriUHD/homehub/internal/store"
	"github.com/TheOriUHD/homehub/internal/term"
)

// ---- backups, tasks, storage ----------------------------------------------

func (s *Server) PveBackupGuest(ctx context.Context, req PveBackupGuestRequestObject) (PveBackupGuestResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, _, resource, err := s.pveConn(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return PveBackupGuest404JSONResponse{notFoundErr("integration")}, nil
	}
	if err != nil {
		return PveBackupGuest502JSONResponse{UpstreamErrorJSONResponse(errBody("unreachable", err.Error()))}, nil
	}
	guestRes := guestResource(resource, req.Vmid)
	if !s.can(ctx, id, guestRes, rbac.LevelControl) {
		s.deniedAudit(ctx, id, "proxmox.backup_now", guestRes)
		return PveBackupGuest403JSONResponse{forbidden()}, nil
	}
	node, _, err := conn.Locate(ctx, req.Vmid)
	if err != nil {
		return PveBackupGuest404JSONResponse{notFoundErr("guest")}, nil
	}
	mode := "snapshot"
	if req.Body.Mode != nil {
		mode = string(*req.Body.Mode)
	}
	compress := "zstd"
	if req.Body.Compress != nil {
		compress = string(*req.Body.Compress)
	}
	upid, err := conn.Client.Vzdump(ctx, node, req.Vmid, req.Body.Storage, mode, compress)
	if err != nil {
		s.audit.Log(ctx, actorFor(id), "proxmox.backup_now", guestRes, "failure", map[string]any{"error": err.Error()})
		return PveBackupGuest502JSONResponse{UpstreamErrorJSONResponse(upstreamErr(err))}, nil
	}
	s.audit.Log(ctx, actorFor(id), "proxmox.backup_now", guestRes, "success",
		map[string]any{"storage": req.Body.Storage, "mode": mode, "upid": upid})
	return PveBackupGuest200JSONResponse{TaskStartedJSONResponse{Upid: upid, Node: node}}, nil
}

func (s *Server) PveListTasks(ctx context.Context, req PveListTasksRequestObject) (PveListTasksResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, _, resource, err := s.pveConn(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return PveListTasks404JSONResponse{notFoundErr("integration")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelView) {
		return PveListTasks403JSONResponse{forbidden()}, nil
	}
	if err != nil {
		return PveListTasks502JSONResponse{UpstreamErrorJSONResponse(errBody("unreachable", err.Error()))}, nil
	}
	limit := 50
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	status, err := conn.Client.ClusterStatus(ctx)
	if err != nil {
		return PveListTasks502JSONResponse{UpstreamErrorJSONResponse(upstreamErr(err))}, nil
	}
	out := []PveTask{}
	for _, entry := range status {
		if entry.Type != "node" || !bool(entry.Online) {
			continue
		}
		tasks, err := conn.Client.NodeTasks(ctx, entry.Name, limit)
		if err != nil {
			continue
		}
		for _, task := range tasks {
			item := PveTask{
				Upid:      task.UPID,
				Node:      entry.Name,
				Type:      task.Type,
				Id:        task.ID,
				User:      task.User,
				Running:   task.Status == "running" || (task.Status == "" && int64(task.EndTime) == 0),
				StartTime: time.Unix(int64(task.StartTime), 0),
			}
			// Real PVE's node task list carries the exit verdict in `status`
			// ("OK" or the error text) for finished tasks; a separate
			// exitstatus field only exists on the task detail endpoint.
			if !item.Running {
				exit := task.ExitStatus
				if exit == "" && task.Status != "" && task.Status != "stopped" {
					exit = task.Status
				}
				if exit != "" {
					item.ExitStatus = &exit
				}
			}
			if int64(task.EndTime) > 0 {
				end := time.Unix(int64(task.EndTime), 0)
				item.EndTime = &end
			}
			out = append(out, item)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartTime.After(out[j].StartTime) })
	if len(out) > limit {
		out = out[:limit]
	}
	return PveListTasks200JSONResponse(out), nil
}

func (s *Server) PveTaskLog(ctx context.Context, req PveTaskLogRequestObject) (PveTaskLogResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, _, resource, err := s.pveConn(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return PveTaskLog404JSONResponse{notFoundErr("integration")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelView) {
		return PveTaskLog403JSONResponse{forbidden()}, nil
	}
	if err != nil {
		return PveTaskLog502JSONResponse{UpstreamErrorJSONResponse(errBody("unreachable", err.Error()))}, nil
	}
	status, err := conn.Client.TaskStatus(ctx, req.Node, req.Upid)
	if err != nil {
		return PveTaskLog502JSONResponse{UpstreamErrorJSONResponse(upstreamErr(err))}, nil
	}
	lines, err := conn.Client.TaskLog(ctx, req.Node, req.Upid, 0)
	if err != nil {
		return PveTaskLog502JSONResponse{UpstreamErrorJSONResponse(upstreamErr(err))}, nil
	}
	detail := PveTaskDetail{
		Task: PveTask{
			Upid:      status.UPID,
			Node:      req.Node,
			Type:      status.Type,
			Id:        status.ID,
			User:      status.User,
			Running:   status.Status == "running",
			StartTime: time.Unix(int64(status.StartTime), 0),
		},
	}
	if status.ExitStatus != "" {
		exit := status.ExitStatus
		detail.Task.ExitStatus = &exit
	}
	for _, line := range lines {
		detail.Lines = append(detail.Lines, line.T)
	}
	return PveTaskLog200JSONResponse(detail), nil
}

func (s *Server) PveListStorage(ctx context.Context, req PveListStorageRequestObject) (PveListStorageResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, _, resource, err := s.pveConn(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return PveListStorage404JSONResponse{notFoundErr("integration")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelView) {
		return PveListStorage403JSONResponse{forbidden()}, nil
	}
	if err != nil {
		return PveListStorage502JSONResponse{UpstreamErrorJSONResponse(errBody("unreachable", err.Error()))}, nil
	}
	status, err := conn.Client.ClusterStatus(ctx)
	if err != nil {
		return PveListStorage502JSONResponse{UpstreamErrorJSONResponse(upstreamErr(err))}, nil
	}
	out := []PveStorage{}
	for _, entry := range status {
		if entry.Type != "node" || !bool(entry.Online) {
			continue
		}
		pools, err := conn.Client.NodeStorage(ctx, entry.Name)
		if err != nil {
			continue
		}
		for _, pool := range pools {
			out = append(out, PveStorage{
				Storage:    pool.Storage,
				Node:       entry.Name,
				Type:       pool.Type,
				Content:    pool.Content,
				Active:     bool(pool.Active),
				UsedBytes:  int64(pool.Used),
				TotalBytes: int64(pool.Total),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Storage != out[j].Storage {
			return out[i].Storage < out[j].Storage
		}
		return out[i].Node < out[j].Node
	})
	return PveListStorage200JSONResponse(out), nil
}

func (s *Server) PveListBackupJobs(ctx context.Context, req PveListBackupJobsRequestObject) (PveListBackupJobsResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, _, resource, err := s.pveConn(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return PveListBackupJobs404JSONResponse{notFoundErr("integration")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelView) {
		return PveListBackupJobs403JSONResponse{forbidden()}, nil
	}
	if err != nil {
		return PveListBackupJobs502JSONResponse{UpstreamErrorJSONResponse(errBody("unreachable", err.Error()))}, nil
	}
	jobs, err := conn.Client.BackupJobs(ctx)
	if err != nil {
		return PveListBackupJobs502JSONResponse{UpstreamErrorJSONResponse(upstreamErr(err))}, nil
	}
	out := []PveBackupJob{}
	for _, job := range jobs {
		vmids := job.VMID
		if bool(job.All) {
			vmids = ""
		}
		out = append(out, PveBackupJob{
			Id:       job.ID,
			Schedule: job.Schedule,
			Storage:  job.Storage,
			Mode:     job.Mode,
			Enabled:  bool(job.Enabled),
			Vmids:    vmids,
		})
	}
	return PveListBackupJobs200JSONResponse(out), nil
}

func (s *Server) PveCreateBackupJob(ctx context.Context, req PveCreateBackupJobRequestObject) (PveCreateBackupJobResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, _, resource, err := s.pveConn(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return PveCreateBackupJob404JSONResponse{notFoundErr("integration")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "proxmox.backup_job_create", resource)
		return PveCreateBackupJob403JSONResponse{forbidden()}, nil
	}
	if err != nil {
		return PveCreateBackupJob502JSONResponse{UpstreamErrorJSONResponse(errBody("unreachable", err.Error()))}, nil
	}
	spec := backupSpecFromAPI(req.Body)
	if err := conn.Client.CreateBackupJob(ctx, spec); err != nil {
		s.audit.Log(ctx, actorFor(id), "proxmox.backup_job_create", resource, "failure", map[string]any{"error": err.Error()})
		return PveCreateBackupJob502JSONResponse{UpstreamErrorJSONResponse(upstreamErr(err))}, nil
	}
	s.audit.Log(ctx, actorFor(id), "proxmox.backup_job_create", resource, "success",
		map[string]any{"schedule": spec.Schedule, "storage": spec.Storage, "vmids": spec.VMID})
	return PveCreateBackupJob204Response{}, nil
}

func (s *Server) PveUpdateBackupJob(ctx context.Context, req PveUpdateBackupJobRequestObject) (PveUpdateBackupJobResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, _, resource, err := s.pveConn(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return PveUpdateBackupJob404JSONResponse{notFoundErr("integration")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "proxmox.backup_job_update", resource)
		return PveUpdateBackupJob403JSONResponse{forbidden()}, nil
	}
	if err != nil {
		return PveUpdateBackupJob502JSONResponse{UpstreamErrorJSONResponse(errBody("unreachable", err.Error()))}, nil
	}
	spec := backupSpecFromAPI(req.Body)
	if err := conn.Client.UpdateBackupJob(ctx, req.JobId, spec); err != nil {
		s.audit.Log(ctx, actorFor(id), "proxmox.backup_job_update", resource, "failure", map[string]any{"error": err.Error()})
		return PveUpdateBackupJob502JSONResponse{UpstreamErrorJSONResponse(upstreamErr(err))}, nil
	}
	s.audit.Log(ctx, actorFor(id), "proxmox.backup_job_update", resource, "success", map[string]any{"job": req.JobId})
	return PveUpdateBackupJob204Response{}, nil
}

// ---- bulk actions ----------------------------------------------------------

func (s *Server) PveBulkAction(ctx context.Context, req PveBulkActionRequestObject) (PveBulkActionResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, _, resource, err := s.pveConn(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return PveBulkAction404JSONResponse{notFoundErr("integration")}, nil
	}
	action := string(req.Body.Action)
	resp := PveBulkAction200JSONResponse{}
	type bulkRow = struct {
		Error *string `json:"error,omitempty"`
		Upid  *string `json:"upid,omitempty"`
		Vmid  int     `json:"vmid"`
	}
	fail := func(vmid int, msg string) {
		resp.Results = append(resp.Results, bulkRow{Vmid: vmid, Error: &msg})
	}
	for _, vmid := range req.Body.Vmids {
		if err != nil {
			fail(vmid, err.Error())
			continue
		}
		guestRes := guestResource(resource, vmid)
		if !s.can(ctx, id, guestRes, rbac.LevelControl) {
			s.deniedAudit(ctx, id, "proxmox.guest_"+action, guestRes)
			fail(vmid, "not permitted")
			continue
		}
		node, kind, lerr := conn.Locate(ctx, vmid)
		if lerr != nil {
			fail(vmid, "guest not found")
			continue
		}
		upid, aerr := conn.Client.GuestAction(ctx, node, kind, vmid, action)
		if aerr != nil {
			s.audit.Log(ctx, actorFor(id), "proxmox.guest_"+action, guestRes, "failure", map[string]any{"bulk": true, "error": aerr.Error()})
			fail(vmid, aerr.Error())
			continue
		}
		s.audit.Log(ctx, actorFor(id), "proxmox.guest_"+action, guestRes, "success", map[string]any{"bulk": true, "upid": upid})
		u := upid
		resp.Results = append(resp.Results, bulkRow{Vmid: vmid, Upid: &u})
	}
	return resp, nil
}

// ---- consoles and terminals ------------------------------------------------

func (s *Server) PveOpenConsole(ctx context.Context, req PveOpenConsoleRequestObject) (PveOpenConsoleResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, _, resource, err := s.pveConn(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return PveOpenConsole404JSONResponse{notFoundErr("integration")}, nil
	}
	if err != nil {
		return PveOpenConsole502JSONResponse{UpstreamErrorJSONResponse(errBody("unreachable", err.Error()))}, nil
	}
	guestRes := guestResource(resource, req.Vmid)
	if !s.can(ctx, id, guestRes, rbac.LevelControl) {
		s.deniedAudit(ctx, id, "proxmox.console", guestRes)
		return PveOpenConsole403JSONResponse{forbidden()}, nil
	}
	node, kind, err := conn.Locate(ctx, req.Vmid)
	if err != nil {
		return PveOpenConsole404JSONResponse{notFoundErr("guest")}, nil
	}

	session := TermSession{}
	switch req.Body.Kind {
	case "vnc":
		ticket, err := conn.Client.VNCProxy(ctx, node, kind, req.Vmid, true)
		if err != nil {
			return PveOpenConsole502JSONResponse{UpstreamErrorJSONResponse(upstreamErr(err))}, nil
		}
		sessionID := s.term.Create(id.User.ID, term.OpenPVEVNC(conn.Client, node, kind, req.Vmid, ticket))
		credential := ticket.Ticket
		session = TermSession{SessionId: sessionID, WsPath: "/api/term/" + sessionID, Credential: &credential}
	default: // serial
		ticket, err := conn.Client.TermProxy(ctx, node, kind, req.Vmid)
		if err != nil {
			return PveOpenConsole502JSONResponse{UpstreamErrorJSONResponse(upstreamErr(err))}, nil
		}
		sessionID := s.term.Create(id.User.ID, term.OpenPVESerial(conn.Client, node, kind, req.Vmid, ticket))
		session = TermSession{SessionId: sessionID, WsPath: "/api/term/" + sessionID}
	}
	s.audit.Log(ctx, actorFor(id), "proxmox.console", guestRes, "success", map[string]any{"kind": req.Body.Kind})
	return PveOpenConsole200JSONResponse(session), nil
}

func (s *Server) PveOpenSSH(ctx context.Context, req PveOpenSSHRequestObject) (PveOpenSSHResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	_, _, resource, err := s.pveConn(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return PveOpenSSH404JSONResponse{notFoundErr("integration")}, nil
	}
	guestRes := guestResource(resource, req.Vmid)
	if !s.can(ctx, id, guestRes, rbac.LevelControl) {
		s.deniedAudit(ctx, id, "proxmox.ssh", guestRes)
		return PveOpenSSH403JSONResponse{forbidden()}, nil
	}

	// One resolver for every SSH consumer: a bound registry host wins,
	// otherwise this guest's own inline credential.
	target, err := s.ResolveSSHForResource(ctx, guestRes)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return PveOpenSSH400JSONResponse{BadRequestJSONResponse(errBody("no_ssh_credential",
				"no SSH access configured for this guest — pick an SSH host or add credentials under the guest's SSH tab"))}, nil
		}
		return PveOpenSSH400JSONResponse{BadRequestJSONResponse(errBody("no_ssh_credential", err.Error()))}, nil
	}

	sessionID := s.term.Create(id.User.ID, term.OpenSSH(target))
	s.audit.Log(ctx, actorFor(id), "proxmox.ssh", guestRes, "success", nil)
	return PveOpenSSH200JSONResponse(TermSession{SessionId: sessionID, WsPath: "/api/term/" + sessionID}), nil
}

func backupSpecFromAPI(body *PveBackupJobSpec) pve.BackupJobSpec {
	spec := pve.BackupJobSpec{
		Schedule: body.Schedule,
		Storage:  body.Storage,
		VMID:     body.Vmids,
		Mode:     "snapshot",
		Enabled:  true,
	}
	if body.Mode != nil {
		spec.Mode = string(*body.Mode)
	}
	if body.Enabled != nil {
		spec.Enabled = *body.Enabled
	}
	if body.Vmids == "" {
		spec.All = true
	}
	return spec
}
