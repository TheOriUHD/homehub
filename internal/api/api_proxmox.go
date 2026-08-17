package api

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/TheOriUHD/homehub/internal/httpctx"
	"github.com/TheOriUHD/homehub/internal/integrations/proxmox"
	"github.com/TheOriUHD/homehub/internal/rbac"
	"github.com/TheOriUHD/homehub/internal/store"
)

// pveConn resolves an integration id to a live Proxmox connection, verifying
// the instance exists and is of the right type. Returns a user-facing error
// message when the connection is down.
func (s *Server) pveConn(ctx context.Context, id *httpctx.Identity, instID uuid.UUID) (*proxmox.Conn, *store.Integration, string, error) {
	row, err := s.store.GetIntegration(ctx, instID)
	if err != nil || row.Type != proxmox.TypeID {
		return nil, nil, "", store.ErrNotFound
	}
	resource := proxmox.TypeID + "/" + instID.String()
	if !s.visibleUnder(ctx, id, resource) {
		return nil, nil, "", store.ErrNotFound
	}
	conn, err := s.integrations.Connection(instID)
	if err != nil {
		return nil, row, resource, err
	}
	pc, ok := conn.(*proxmox.Conn)
	if !ok {
		return nil, row, resource, errors.New("integration connection has unexpected type")
	}
	return pc, row, resource, nil
}

func upstreamErr(err error) Error {
	return errBody("upstream", proxmox.FriendlyError(err))
}

// guestResource builds the RBAC path for a guest.
func guestResource(resource string, vmid int) string {
	return fmt.Sprintf("%s/guest/%d", resource, vmid)
}

// ---- overview and listings -------------------------------------------------

func (s *Server) PveSummary(ctx context.Context, req PveSummaryRequestObject) (PveSummaryResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, _, resource, err := s.pveConn(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return PveSummary404JSONResponse{notFoundErr("integration")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelView) {
		return PveSummary403JSONResponse{forbidden()}, nil
	}
	if err != nil {
		return PveSummary502JSONResponse{UpstreamErrorJSONResponse(errBody("unreachable", err.Error()))}, nil
	}

	status, err := conn.Client.ClusterStatus(ctx)
	if err != nil {
		return PveSummary502JSONResponse{UpstreamErrorJSONResponse(upstreamErr(err))}, nil
	}
	resources, err := conn.Client.ClusterResources(ctx)
	if err != nil {
		return PveSummary502JSONResponse{UpstreamErrorJSONResponse(upstreamErr(err))}, nil
	}

	summary := PveSummary{ClusterName: "", Quorate: nil}
	for _, entry := range status {
		if entry.Type == "cluster" {
			summary.ClusterName = entry.Name
			q := bool(entry.Quorate)
			summary.Quorate = &q
		}
	}

	versions := map[string]string{}
	for _, entry := range status {
		if entry.Type == "node" && bool(entry.Online) {
			if ns, err := conn.Client.NodeStatus(ctx, entry.Name); err == nil {
				versions[entry.Name] = ns.PVEVersion
			}
		}
	}

	seenStorage := map[string]bool{}
	for _, r := range resources {
		switch r.Type {
		case "node":
			node := PveNode{
				Name:          r.Node,
				Status:        r.Status,
				Cpu:           float32(r.CPU),
				MaxCpu:        int(r.MaxCPU),
				MemBytes:      int64(r.Mem),
				MaxMemBytes:   int64(r.MaxMem),
				UptimeSeconds: int64(r.Uptime),
			}
			if v, ok := versions[r.Node]; ok && v != "" {
				node.PveVersion = &v
			}
			summary.Nodes = append(summary.Nodes, node)
		case "storage":
			// Shared storages appear once per node; count each pool once.
			if !seenStorage[r.Storage] {
				seenStorage[r.Storage] = true
				summary.StorageUsedBytes += int64(r.Disk)
				summary.StorageTotalBytes += int64(r.MaxDisk)
			}
		}
	}
	sort.Slice(summary.Nodes, func(i, j int) bool { return summary.Nodes[i].Name < summary.Nodes[j].Name })
	return PveSummary200JSONResponse(summary), nil
}

func (s *Server) PveListGuests(ctx context.Context, req PveListGuestsRequestObject) (PveListGuestsResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, _, resource, err := s.pveConn(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return PveListGuests404JSONResponse{notFoundErr("integration")}, nil
	}
	if err != nil {
		return PveListGuests502JSONResponse{UpstreamErrorJSONResponse(errBody("unreachable", err.Error()))}, nil
	}
	rows, err := conn.Client.ClusterResources(ctx)
	if err != nil {
		return PveListGuests502JSONResponse{UpstreamErrorJSONResponse(upstreamErr(err))}, nil
	}
	out := []PveGuest{}
	for _, r := range rows {
		if r.Type != "qemu" && r.Type != "lxc" {
			continue
		}
		vmid := int(r.VMID)
		if !s.can(ctx, id, guestResource(resource, vmid), rbac.LevelView) {
			continue
		}
		guest := PveGuest{
			Vmid:          vmid,
			Name:          r.Name,
			Kind:          PveGuestKind(r.Type),
			Node:          r.Node,
			Status:        r.Status,
			Cpu:           float32(r.CPU),
			MaxCpu:        int(r.MaxCPU),
			MemBytes:      int64(r.Mem),
			MaxMemBytes:   int64(r.MaxMem),
			DiskBytes:     int64(r.Disk),
			MaxDiskBytes:  int64(r.MaxDisk),
			UptimeSeconds: int64(r.Uptime),
			Template:      bool(r.Template),
		}
		if r.Tags != "" {
			tags := r.Tags
			guest.Tags = &tags
		}
		out = append(out, guest)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Vmid < out[j].Vmid })
	return PveListGuests200JSONResponse(out), nil
}

func (s *Server) PveGetGuest(ctx context.Context, req PveGetGuestRequestObject) (PveGetGuestResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, _, resource, err := s.pveConn(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return PveGetGuest404JSONResponse{notFoundErr("integration")}, nil
	}
	if err != nil {
		return PveGetGuest502JSONResponse{UpstreamErrorJSONResponse(errBody("unreachable", err.Error()))}, nil
	}
	if !s.can(ctx, id, guestResource(resource, req.Vmid), rbac.LevelView) {
		return PveGetGuest403JSONResponse{forbidden()}, nil
	}
	node, kind, err := conn.Locate(ctx, req.Vmid)
	if err != nil {
		return PveGetGuest404JSONResponse{notFoundErr("guest")}, nil
	}
	current, err := conn.Client.GuestStatus(ctx, node, kind, req.Vmid)
	if err != nil {
		return PveGetGuest502JSONResponse{UpstreamErrorJSONResponse(upstreamErr(err))}, nil
	}
	config, err := conn.Client.GuestConfig(ctx, node, kind, req.Vmid)
	if err != nil {
		return PveGetGuest502JSONResponse{UpstreamErrorJSONResponse(upstreamErr(err))}, nil
	}
	name := current.Name
	if name == "" {
		if v, ok := config["name"].(string); ok {
			name = v
		} else if v, ok := config["hostname"].(string); ok {
			name = v
		}
	}
	detail := PveGuestDetail{
		Guest: PveGuest{
			Vmid:          req.Vmid,
			Name:          name,
			Kind:          PveGuestKind(kind),
			Node:          node,
			Status:        current.Status,
			Cpu:           float32(current.CPU),
			MaxCpu:        int(current.CPUs),
			MemBytes:      int64(current.Mem),
			MaxMemBytes:   int64(current.MaxMem),
			DiskBytes:     int64(current.Disk),
			MaxDiskBytes:  int64(current.MaxDisk),
			UptimeSeconds: int64(current.Uptime),
			Template:      false,
		},
		Config: config,
	}
	if current.Tags != "" {
		tags := current.Tags
		detail.Guest.Tags = &tags
	}
	return PveGetGuest200JSONResponse(detail), nil
}

// ---- lifecycle -------------------------------------------------------------

func (s *Server) PveGuestAction(ctx context.Context, req PveGuestActionRequestObject) (PveGuestActionResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, _, resource, err := s.pveConn(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return PveGuestAction404JSONResponse{notFoundErr("integration")}, nil
	}
	if err != nil {
		return PveGuestAction502JSONResponse{UpstreamErrorJSONResponse(errBody("unreachable", err.Error()))}, nil
	}
	guestRes := guestResource(resource, req.Vmid)
	action := string(req.Body.Action)
	if !s.can(ctx, id, guestRes, rbac.LevelControl) {
		s.deniedAudit(ctx, id, "proxmox.guest_"+action, guestRes)
		return PveGuestAction403JSONResponse{forbidden()}, nil
	}
	node, kind, err := conn.Locate(ctx, req.Vmid)
	if err != nil {
		return PveGuestAction404JSONResponse{notFoundErr("guest")}, nil
	}
	upid, err := conn.Client.GuestAction(ctx, node, kind, req.Vmid, action)
	if err != nil {
		s.audit.Log(ctx, actorFor(id), "proxmox.guest_"+action, guestRes, "failure", map[string]any{"error": err.Error()})
		return PveGuestAction502JSONResponse{UpstreamErrorJSONResponse(upstreamErr(err))}, nil
	}
	s.audit.Log(ctx, actorFor(id), "proxmox.guest_"+action, guestRes, "success", map[string]any{"upid": upid})
	return PveGuestAction200JSONResponse{TaskStartedJSONResponse{Upid: upid, Node: node}}, nil
}

func (s *Server) PveSetGuestConfig(ctx context.Context, req PveSetGuestConfigRequestObject) (PveSetGuestConfigResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, _, resource, err := s.pveConn(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return PveSetGuestConfig404JSONResponse{notFoundErr("integration")}, nil
	}
	if err != nil {
		return PveSetGuestConfig502JSONResponse{UpstreamErrorJSONResponse(errBody("unreachable", err.Error()))}, nil
	}
	guestRes := guestResource(resource, req.Vmid)
	if !s.can(ctx, id, guestRes, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "proxmox.guest_config", guestRes)
		return PveSetGuestConfig403JSONResponse{forbidden()}, nil
	}
	if len(req.Body.Changes) == 0 {
		return PveSetGuestConfig400JSONResponse{BadRequestJSONResponse(errBody("empty", "no configuration changes provided"))}, nil
	}
	node, kind, err := conn.Locate(ctx, req.Vmid)
	if err != nil {
		return PveSetGuestConfig404JSONResponse{notFoundErr("guest")}, nil
	}
	if err := conn.Client.SetGuestConfig(ctx, node, kind, req.Vmid, req.Body.Changes); err != nil {
		s.audit.Log(ctx, actorFor(id), "proxmox.guest_config", guestRes, "failure", map[string]any{"error": err.Error()})
		return PveSetGuestConfig502JSONResponse{UpstreamErrorJSONResponse(upstreamErr(err))}, nil
	}
	keys := make([]string, 0, len(req.Body.Changes))
	for k := range req.Body.Changes {
		keys = append(keys, k)
	}
	s.audit.Log(ctx, actorFor(id), "proxmox.guest_config", guestRes, "success", map[string]any{"keys": keys})
	return PveSetGuestConfig204Response{}, nil
}

func (s *Server) PveMigrateGuest(ctx context.Context, req PveMigrateGuestRequestObject) (PveMigrateGuestResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, _, resource, err := s.pveConn(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return PveMigrateGuest404JSONResponse{notFoundErr("integration")}, nil
	}
	if err != nil {
		return PveMigrateGuest502JSONResponse{UpstreamErrorJSONResponse(errBody("unreachable", err.Error()))}, nil
	}
	guestRes := guestResource(resource, req.Vmid)
	if !s.can(ctx, id, guestRes, rbac.LevelControl) {
		s.deniedAudit(ctx, id, "proxmox.guest_migrate", guestRes)
		return PveMigrateGuest403JSONResponse{forbidden()}, nil
	}
	node, kind, err := conn.Locate(ctx, req.Vmid)
	if err != nil {
		return PveMigrateGuest404JSONResponse{notFoundErr("guest")}, nil
	}
	online := req.Body.Online != nil && *req.Body.Online
	upid, err := conn.Client.MigrateGuest(ctx, node, kind, req.Vmid, req.Body.Target, online)
	if err != nil {
		s.audit.Log(ctx, actorFor(id), "proxmox.guest_migrate", guestRes, "failure", map[string]any{"error": err.Error()})
		return PveMigrateGuest502JSONResponse{UpstreamErrorJSONResponse(upstreamErr(err))}, nil
	}
	s.audit.Log(ctx, actorFor(id), "proxmox.guest_migrate", guestRes, "success",
		map[string]any{"target": req.Body.Target, "online": online, "upid": upid})
	return PveMigrateGuest200JSONResponse{TaskStartedJSONResponse{Upid: upid, Node: node}}, nil
}

func (s *Server) PveCloneGuest(ctx context.Context, req PveCloneGuestRequestObject) (PveCloneGuestResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, _, resource, err := s.pveConn(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return PveCloneGuest404JSONResponse{notFoundErr("integration")}, nil
	}
	if err != nil {
		return PveCloneGuest502JSONResponse{UpstreamErrorJSONResponse(errBody("unreachable", err.Error()))}, nil
	}
	// Cloning creates a new guest — an instance-level configure right.
	if !s.can(ctx, id, resource, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "proxmox.guest_clone", resource)
		return PveCloneGuest403JSONResponse{forbidden()}, nil
	}
	node, kind, err := conn.Locate(ctx, req.Vmid)
	if err != nil {
		return PveCloneGuest404JSONResponse{notFoundErr("guest")}, nil
	}
	name := ""
	if req.Body.Name != nil {
		name = *req.Body.Name
	}
	full := req.Body.Full != nil && *req.Body.Full
	target := ""
	if req.Body.Target != nil {
		target = *req.Body.Target
	}
	upid, err := conn.Client.CloneGuest(ctx, node, kind, req.Vmid, req.Body.Newid, name, full, target)
	if err != nil {
		s.audit.Log(ctx, actorFor(id), "proxmox.guest_clone", resource, "failure", map[string]any{"error": err.Error()})
		return PveCloneGuest502JSONResponse{UpstreamErrorJSONResponse(upstreamErr(err))}, nil
	}
	s.audit.Log(ctx, actorFor(id), "proxmox.guest_clone", resource, "success",
		map[string]any{"source": req.Vmid, "newid": req.Body.Newid, "upid": upid})
	return PveCloneGuest200JSONResponse{TaskStartedJSONResponse{Upid: upid, Node: node}}, nil
}

func (s *Server) PveDeleteGuest(ctx context.Context, req PveDeleteGuestRequestObject) (PveDeleteGuestResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, _, resource, err := s.pveConn(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return PveDeleteGuest404JSONResponse{notFoundErr("integration")}, nil
	}
	if err != nil {
		return PveDeleteGuest502JSONResponse{UpstreamErrorJSONResponse(errBody("unreachable", err.Error()))}, nil
	}
	guestRes := guestResource(resource, req.Vmid)
	if !s.can(ctx, id, guestRes, rbac.LevelDelete) {
		s.deniedAudit(ctx, id, "proxmox.guest_delete", guestRes)
		return PveDeleteGuest403JSONResponse{forbidden()}, nil
	}
	node, kind, err := conn.Locate(ctx, req.Vmid)
	if err != nil {
		return PveDeleteGuest404JSONResponse{notFoundErr("guest")}, nil
	}
	purge := req.Params.Purge != nil && *req.Params.Purge
	upid, err := conn.Client.DeleteGuest(ctx, node, kind, req.Vmid, purge)
	if err != nil {
		s.audit.Log(ctx, actorFor(id), "proxmox.guest_delete", guestRes, "failure", map[string]any{"error": err.Error()})
		return PveDeleteGuest502JSONResponse{UpstreamErrorJSONResponse(upstreamErr(err))}, nil
	}
	s.audit.Log(ctx, actorFor(id), "proxmox.guest_delete", guestRes, "success", map[string]any{"purge": purge, "upid": upid})
	return PveDeleteGuest200JSONResponse{TaskStartedJSONResponse{Upid: upid, Node: node}}, nil
}

// ---- snapshots -------------------------------------------------------------

func (s *Server) PveListSnapshots(ctx context.Context, req PveListSnapshotsRequestObject) (PveListSnapshotsResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, _, resource, err := s.pveConn(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return PveListSnapshots404JSONResponse{notFoundErr("integration")}, nil
	}
	if err != nil {
		return PveListSnapshots502JSONResponse{UpstreamErrorJSONResponse(errBody("unreachable", err.Error()))}, nil
	}
	if !s.can(ctx, id, guestResource(resource, req.Vmid), rbac.LevelView) {
		return PveListSnapshots403JSONResponse{forbidden()}, nil
	}
	node, kind, err := conn.Locate(ctx, req.Vmid)
	if err != nil {
		return PveListSnapshots404JSONResponse{notFoundErr("guest")}, nil
	}
	snapshots, err := conn.Client.ListSnapshots(ctx, node, kind, req.Vmid)
	if err != nil {
		return PveListSnapshots502JSONResponse{UpstreamErrorJSONResponse(upstreamErr(err))}, nil
	}
	out := []PveSnapshot{}
	for _, snap := range snapshots {
		item := PveSnapshot{Name: snap.Name, Description: snap.Description, Vmstate: bool(snap.VMState)}
		if snap.SnapTime > 0 {
			t := time.Unix(int64(snap.SnapTime), 0)
			item.Snaptime = &t
		}
		out = append(out, item)
	}
	return PveListSnapshots200JSONResponse(out), nil
}

func (s *Server) PveCreateSnapshot(ctx context.Context, req PveCreateSnapshotRequestObject) (PveCreateSnapshotResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, _, resource, err := s.pveConn(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return PveCreateSnapshot404JSONResponse{notFoundErr("integration")}, nil
	}
	if err != nil {
		return PveCreateSnapshot502JSONResponse{UpstreamErrorJSONResponse(errBody("unreachable", err.Error()))}, nil
	}
	guestRes := guestResource(resource, req.Vmid)
	if !s.can(ctx, id, guestRes, rbac.LevelControl) {
		s.deniedAudit(ctx, id, "proxmox.snapshot_create", guestRes)
		return PveCreateSnapshot403JSONResponse{forbidden()}, nil
	}
	node, kind, err := conn.Locate(ctx, req.Vmid)
	if err != nil {
		return PveCreateSnapshot404JSONResponse{notFoundErr("guest")}, nil
	}
	description := ""
	if req.Body.Description != nil {
		description = *req.Body.Description
	}
	vmstate := req.Body.Vmstate != nil && *req.Body.Vmstate
	upid, err := conn.Client.CreateSnapshot(ctx, node, kind, req.Vmid, req.Body.Name, description, vmstate)
	if err != nil {
		s.audit.Log(ctx, actorFor(id), "proxmox.snapshot_create", guestRes, "failure", map[string]any{"error": err.Error()})
		return PveCreateSnapshot502JSONResponse{UpstreamErrorJSONResponse(upstreamErr(err))}, nil
	}
	s.audit.Log(ctx, actorFor(id), "proxmox.snapshot_create", guestRes, "success",
		map[string]any{"snapshot": req.Body.Name, "upid": upid})
	return PveCreateSnapshot200JSONResponse{TaskStartedJSONResponse{Upid: upid, Node: node}}, nil
}

func (s *Server) PveRollbackSnapshot(ctx context.Context, req PveRollbackSnapshotRequestObject) (PveRollbackSnapshotResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, _, resource, err := s.pveConn(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return PveRollbackSnapshot404JSONResponse{notFoundErr("integration")}, nil
	}
	if err != nil {
		return PveRollbackSnapshot502JSONResponse{UpstreamErrorJSONResponse(errBody("unreachable", err.Error()))}, nil
	}
	guestRes := guestResource(resource, req.Vmid)
	if !s.can(ctx, id, guestRes, rbac.LevelControl) {
		s.deniedAudit(ctx, id, "proxmox.snapshot_rollback", guestRes)
		return PveRollbackSnapshot403JSONResponse{forbidden()}, nil
	}
	node, kind, err := conn.Locate(ctx, req.Vmid)
	if err != nil {
		return PveRollbackSnapshot404JSONResponse{notFoundErr("guest")}, nil
	}
	upid, err := conn.Client.RollbackSnapshot(ctx, node, kind, req.Vmid, req.Name)
	if err != nil {
		s.audit.Log(ctx, actorFor(id), "proxmox.snapshot_rollback", guestRes, "failure", map[string]any{"error": err.Error()})
		return PveRollbackSnapshot502JSONResponse{UpstreamErrorJSONResponse(upstreamErr(err))}, nil
	}
	s.audit.Log(ctx, actorFor(id), "proxmox.snapshot_rollback", guestRes, "success",
		map[string]any{"snapshot": req.Name, "upid": upid})
	return PveRollbackSnapshot200JSONResponse{TaskStartedJSONResponse{Upid: upid, Node: node}}, nil
}

func (s *Server) PveDeleteSnapshot(ctx context.Context, req PveDeleteSnapshotRequestObject) (PveDeleteSnapshotResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, _, resource, err := s.pveConn(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return PveDeleteSnapshot404JSONResponse{notFoundErr("integration")}, nil
	}
	if err != nil {
		return PveDeleteSnapshot502JSONResponse{UpstreamErrorJSONResponse(errBody("unreachable", err.Error()))}, nil
	}
	guestRes := guestResource(resource, req.Vmid)
	if !s.can(ctx, id, guestRes, rbac.LevelControl) {
		s.deniedAudit(ctx, id, "proxmox.snapshot_delete", guestRes)
		return PveDeleteSnapshot403JSONResponse{forbidden()}, nil
	}
	node, kind, err := conn.Locate(ctx, req.Vmid)
	if err != nil {
		return PveDeleteSnapshot404JSONResponse{notFoundErr("guest")}, nil
	}
	upid, err := conn.Client.DeleteSnapshot(ctx, node, kind, req.Vmid, req.Name)
	if err != nil {
		s.audit.Log(ctx, actorFor(id), "proxmox.snapshot_delete", guestRes, "failure", map[string]any{"error": err.Error()})
		return PveDeleteSnapshot502JSONResponse{UpstreamErrorJSONResponse(upstreamErr(err))}, nil
	}
	s.audit.Log(ctx, actorFor(id), "proxmox.snapshot_delete", guestRes, "success",
		map[string]any{"snapshot": req.Name, "upid": upid})
	return PveDeleteSnapshot200JSONResponse{TaskStartedJSONResponse{Upid: upid, Node: node}}, nil
}
