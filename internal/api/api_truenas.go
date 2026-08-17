package api

import (
	"context"
	"time"

	"github.com/google/uuid"

	"homehub/internal/httpctx"
	"homehub/internal/integrations/truenas"
	"homehub/internal/rbac"
	"homehub/internal/store"
)

// truenasConn resolves an integration id to a live TrueNAS connection,
// enforcing instance visibility.
func (s *Server) truenasConn(ctx context.Context, id *httpctx.Identity, instID uuid.UUID) (*truenas.Conn, string, error) {
	row, err := s.store.GetIntegration(ctx, instID)
	if err != nil || row.Type != truenas.TypeID {
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
	tc, ok := conn.(*truenas.Conn)
	if !ok {
		return nil, resource, store.ErrNotFound
	}
	return tc, resource, nil
}

func toAPITruenasInfo(info *truenas.SystemInfo) *TruenasSystemInfo {
	if info == nil {
		return nil
	}
	out := &TruenasSystemInfo{Version: info.Version, Hostname: info.Hostname}
	if info.Uptime != "" {
		out.Uptime = &info.Uptime
	}
	if info.Model != "" {
		out.Model = &info.Model
	}
	if info.Cores > 0 {
		out.Cores = &info.Cores
	}
	if info.PhysicalMem > 0 {
		out.PhysicalMemoryBytes = &info.PhysicalMem
	}
	return out
}

func (s *Server) TruenasListInstances(ctx context.Context, _ TruenasListInstancesRequestObject) (TruenasListInstancesResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	rows, err := s.store.ListIntegrations(ctx)
	if err != nil {
		return nil, err
	}
	out := []TruenasInstance{}
	for _, row := range rows {
		if row.Type != truenas.TypeID || !row.Enabled {
			continue
		}
		resource := row.Type + "/" + row.ID.String()
		if !s.visibleUnder(ctx, id, resource) {
			continue
		}
		instance := TruenasInstance{
			Id: row.ID, Name: row.Name,
			Status: TruenasInstanceStatus(row.Status), StatusMessage: row.StatusMessage,
		}
		if conn, cerr := s.integrations.Connection(row.ID); cerr == nil {
			if tc, ok := conn.(*truenas.Conn); ok {
				ictx, cancel := context.WithTimeout(ctx, 10*time.Second)
				if info, ierr := tc.Info(ictx); ierr == nil {
					instance.Info = toAPITruenasInfo(info)
				}
				if pools, perr := tc.Pools(ictx); perr == nil {
					n := len(s.visibleTruenasPools(ctx, id, resource, pools))
					instance.PoolCount = &n
				}
				cancel()
			}
		}
		out = append(out, instance)
	}
	return TruenasListInstances200JSONResponse(out), nil
}

// visibleTruenasPools filters pools by per-pool view grants.
func (s *Server) visibleTruenasPools(ctx context.Context, id *httpctx.Identity, resource string, pools []truenas.NormPool) []truenas.NormPool {
	out := make([]truenas.NormPool, 0, len(pools))
	for _, p := range pools {
		if s.can(ctx, id, resource+"/pool/"+p.Name, rbac.LevelView) {
			out = append(out, p)
		}
	}
	return out
}

func (s *Server) TruenasOverview(ctx context.Context, req TruenasOverviewRequestObject) (TruenasOverviewResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, resource, err := s.truenasConn(ctx, id, req.Id)
	if err != nil {
		if err == store.ErrNotFound {
			return TruenasOverview404JSONResponse{notFoundErr("instance")}, nil
		}
		return TruenasOverview502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	out := TruenasOverview{Pools: []TruenasPool{}, Disks: []TruenasDisk{}}
	if info, ierr := conn.Info(ctx); ierr == nil {
		out.Info = toAPITruenasInfo(info)
	}
	if mem, merr := conn.Memory(ctx); merr == nil && mem.TotalBytes > 0 {
		out.Memory = &TruenasMemory{
			TotalBytes: mem.TotalBytes, ArcBytes: mem.ArcBytes,
			ServicesBytes: mem.ServicesBytes, FreeBytes: mem.FreeBytes,
		}
	}
	pools, perr := conn.Pools(ctx)
	if perr != nil {
		return TruenasOverview502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", perr.Error()))}, nil
	}
	visiblePools := map[string]bool{}
	for _, p := range s.visibleTruenasPools(ctx, id, resource, pools) {
		visiblePools[p.Name] = true
		out.Pools = append(out.Pools, TruenasPool{
			Name: p.Name, Status: p.Status, Healthy: p.Healthy,
			SizeBytes: p.SizeBytes, AllocatedBytes: p.AllocatedBytes, FreeBytes: p.FreeBytes,
			UsedPct: p.UsedPct, Fragmentation: strPtr(p.Fragmentation),
		})
	}
	// Disks belong to a pool; show only those in visible pools (plus unassigned).
	if disks, derr := conn.Disks(ctx); derr == nil {
		for _, d := range disks {
			if d.Pool != "" && !visiblePools[d.Pool] {
				continue
			}
			disk := TruenasDisk{Name: d.Name, Size: d.Size, Type: d.Type}
			disk.Serial = strPtr(d.Serial)
			disk.Model = strPtr(d.Model)
			disk.Pool = strPtr(d.Pool)
			disk.Temperature = d.Temperature
			out.Disks = append(out.Disks, disk)
		}
	}
	return TruenasOverview200JSONResponse(out), nil
}

func (s *Server) TruenasDatasets(ctx context.Context, req TruenasDatasetsRequestObject) (TruenasDatasetsResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, resource, err := s.truenasConn(ctx, id, req.Id)
	if err != nil {
		if err == store.ErrNotFound {
			return TruenasDatasets404JSONResponse{notFoundErr("instance")}, nil
		}
		return TruenasDatasets502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	datasets, derr := conn.Datasets(ctx)
	if derr != nil {
		return TruenasDatasets502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", derr.Error()))}, nil
	}
	out := []TruenasDataset{}
	for _, d := range datasets {
		// A dataset's root pool is the segment before the first slash.
		if !s.can(ctx, id, resource+"/pool/"+rootPool(d.Name), rbac.LevelView) {
			continue
		}
		out = append(out, TruenasDataset{
			Name: d.Name, Type: d.Type, UsedBytes: d.UsedBytes, AvailableBytes: d.AvailableBytes,
			Compression: strPtr(d.Compression), Mountpoint: strPtr(d.Mountpoint),
		})
	}
	return TruenasDatasets200JSONResponse(out), nil
}

func (s *Server) TruenasShares(ctx context.Context, req TruenasSharesRequestObject) (TruenasSharesResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, _, err := s.truenasConn(ctx, id, req.Id)
	if err != nil {
		if err == store.ErrNotFound {
			return TruenasShares404JSONResponse{notFoundErr("instance")}, nil
		}
		return TruenasShares502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	shares, serr := conn.Shares(ctx)
	if serr != nil {
		return TruenasShares502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", serr.Error()))}, nil
	}
	out := []TruenasShare{}
	for _, sh := range shares {
		share := TruenasShare{Type: TruenasShareType(sh.Type), Name: sh.Name, Enabled: sh.Enabled}
		share.Path = strPtr(sh.Path)
		share.Comment = strPtr(sh.Comment)
		out = append(out, share)
	}
	return TruenasShares200JSONResponse(out), nil
}

func (s *Server) TruenasSnapshots(ctx context.Context, req TruenasSnapshotsRequestObject) (TruenasSnapshotsResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, resource, err := s.truenasConn(ctx, id, req.Id)
	if err != nil {
		if err == store.ErrNotFound {
			return TruenasSnapshots404JSONResponse{notFoundErr("instance")}, nil
		}
		return TruenasSnapshots502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	limit := 100
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	snaps, serr := conn.Snapshots(ctx, limit)
	if serr != nil {
		return TruenasSnapshots502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", serr.Error()))}, nil
	}
	out := []TruenasSnapshot{}
	for _, sn := range snaps {
		if !s.can(ctx, id, resource+"/pool/"+rootPool(sn.Dataset), rbac.LevelView) {
			continue
		}
		snap := TruenasSnapshot{Name: sn.Name, Dataset: sn.Dataset}
		used := sn.Used.Int64()
		snap.UsedBytes = &used
		if sn.Created.Date > 0 {
			ts := time.UnixMilli(sn.Created.Date)
			snap.CreatedAt = &ts
		}
		out = append(out, snap)
	}
	return TruenasSnapshots200JSONResponse(out), nil
}

func (s *Server) TruenasAlerts(ctx context.Context, req TruenasAlertsRequestObject) (TruenasAlertsResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, _, err := s.truenasConn(ctx, id, req.Id)
	if err != nil {
		if err == store.ErrNotFound {
			return TruenasAlerts404JSONResponse{notFoundErr("instance")}, nil
		}
		return TruenasAlerts502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	alerts, aerr := conn.Alerts(ctx)
	if aerr != nil {
		return TruenasAlerts502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", aerr.Error()))}, nil
	}
	out := []TruenasAlert{}
	for _, a := range alerts {
		alert := TruenasAlert{Level: a.Level, Message: a.Formatted, Dismissed: a.Dismissed}
		if a.Datetime.Date > 0 {
			ts := time.UnixMilli(a.Datetime.Date)
			alert.CreatedAt = &ts
		}
		out = append(out, alert)
	}
	return TruenasAlerts200JSONResponse(out), nil
}

// ---- helpers ---------------------------------------------------------------

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func rootPool(datasetName string) string {
	for i := 0; i < len(datasetName); i++ {
		if datasetName[i] == '/' {
			return datasetName[:i]
		}
	}
	return datasetName
}
