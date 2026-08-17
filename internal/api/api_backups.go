package api

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"homehub/internal/backup"
	"homehub/internal/httpctx"
	"homehub/internal/integration"
	"homehub/internal/jobs"
	"homehub/internal/rbac"
	"homehub/internal/store"
)

// ResBackups guards the backup system.
const ResBackups = "system/backups"

func toAPIBackupTarget(row store.BackupTarget, capacity *backup.Capacity) BackupTarget {
	out := BackupTarget{
		Id:            row.ID,
		Type:          BackupTargetType(row.Type),
		Name:          row.Name,
		Config:        integration.ConfigFromJSON(row.Config),
		RetentionDays: row.RetentionDays,
		Encrypt:       row.Encrypt,
		Enabled:       row.Enabled,
		Status:        BackupTargetStatus(row.Status),
		StatusMessage: row.StatusMessage,
	}
	if capacity != nil {
		out.Capacity = &struct {
			FreeBytes  *int64 `json:"freeBytes,omitempty"`
			TotalBytes *int64 `json:"totalBytes,omitempty"`
			Unknown    bool   `json:"unknown"`
		}{Unknown: capacity.Unknown}
		if !capacity.Unknown {
			total, free := capacity.TotalBytes, capacity.FreeBytes
			out.Capacity.TotalBytes = &total
			out.Capacity.FreeBytes = &free
		}
	}
	return out
}

func (s *Server) ListBackupTargets(ctx context.Context, _ ListBackupTargetsRequestObject) (ListBackupTargetsResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResBackups, rbac.LevelView) {
		return ListBackupTargets403JSONResponse{forbidden()}, nil
	}
	rows, err := s.store.ListBackupTargets(ctx)
	if err != nil {
		return nil, err
	}
	out := []BackupTarget{}
	for _, row := range rows {
		var capacity *backup.Capacity
		if row.Enabled {
			capCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
			if target, terr := s.backups.OpenTarget(capCtx, &row); terr == nil {
				if c, cerr := target.Capacity(capCtx); cerr == nil {
					capacity = &c
				}
				target.Close()
			}
			cancel()
		}
		out = append(out, toAPIBackupTarget(row, capacity))
	}
	return ListBackupTargets200JSONResponse(out), nil
}

func (s *Server) TestBackupTargetConfig(ctx context.Context, req TestBackupTargetConfigRequestObject) (TestBackupTargetConfigResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResBackups, rbac.LevelConfigure) {
		return TestBackupTargetConfig403JSONResponse{forbidden()}, nil
	}
	secrets := map[string]string{}
	if req.Body.Secrets != nil {
		secrets = *req.Body.Secrets
	}
	testCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	target, err := backup.Open(testCtx, string(req.Body.Type), req.Body.Config, secrets)
	if err != nil {
		return TestBackupTargetConfig200JSONResponse(TestConnectionResult{Ok: false, Message: err.Error()}), nil
	}
	defer target.Close()
	if err := target.Test(testCtx); err != nil {
		return TestBackupTargetConfig200JSONResponse(TestConnectionResult{Ok: false, Message: err.Error()}), nil
	}
	return TestBackupTargetConfig200JSONResponse(TestConnectionResult{Ok: true, Message: "write test succeeded — the target is usable"}), nil
}

func (s *Server) CreateBackupTarget(ctx context.Context, req CreateBackupTargetRequestObject) (CreateBackupTargetResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResBackups, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "backup.target_create", ResBackups)
		return CreateBackupTarget403JSONResponse{forbidden()}, nil
	}
	name := strings.TrimSpace(req.Body.Name)
	if name == "" {
		return CreateBackupTarget400JSONResponse{BadRequestJSONResponse(errBody("invalid_name", "a name is required"))}, nil
	}
	retention := 30
	if req.Body.RetentionDays != nil {
		retention = *req.Body.RetentionDays
	}
	encrypt := req.Body.Encrypt != nil && *req.Body.Encrypt
	row, err := s.store.CreateBackupTarget(ctx, string(req.Body.Type), name, req.Body.Config, retention, encrypt)
	if err != nil {
		if store.IsUniqueViolation(err) {
			return CreateBackupTarget409JSONResponse{ConflictJSONResponse(errBody("name_taken", "a target with that name already exists"))}, nil
		}
		return nil, err
	}
	if req.Body.Secrets != nil {
		for secretName, value := range *req.Body.Secrets {
			if value == "" {
				continue
			}
			if err := s.vault.Seal(ctx, "target:"+row.ID.String(), secretName, []byte(value)); err != nil {
				return nil, err
			}
		}
	}
	s.audit.Log(ctx, actorFor(id), "backup.target_create", ResBackups, "success",
		map[string]any{"name": name, "type": req.Body.Type})
	return CreateBackupTarget201JSONResponse(toAPIBackupTarget(*row, nil)), nil
}

func (s *Server) UpdateBackupTarget(ctx context.Context, req UpdateBackupTargetRequestObject) (UpdateBackupTargetResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResBackups, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "backup.target_update", ResBackups)
		return UpdateBackupTarget403JSONResponse{forbidden()}, nil
	}
	row, err := s.store.GetBackupTarget(ctx, req.Id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return UpdateBackupTarget404JSONResponse{notFoundErr("backup target")}, nil
		}
		return nil, err
	}
	name := strings.TrimSpace(req.Body.Name)
	if name == "" {
		name = row.Name
	}
	retention := row.RetentionDays
	if req.Body.RetentionDays != nil {
		retention = *req.Body.RetentionDays
	}
	encrypt := row.Encrypt
	if req.Body.Encrypt != nil {
		encrypt = *req.Body.Encrypt
	}
	enabled := row.Enabled
	if req.Body.Enabled != nil {
		enabled = *req.Body.Enabled
	}
	if err := s.store.UpdateBackupTarget(ctx, row.ID, name, req.Body.Config, retention, encrypt, enabled); err != nil {
		return nil, err
	}
	if req.Body.Secrets != nil {
		for secretName, value := range *req.Body.Secrets {
			if value == "" {
				continue
			}
			if err := s.vault.Seal(ctx, "target:"+row.ID.String(), secretName, []byte(value)); err != nil {
				return nil, err
			}
		}
	}
	s.audit.Log(ctx, actorFor(id), "backup.target_update", ResBackups, "success", map[string]any{"name": name})
	fresh, err := s.store.GetBackupTarget(ctx, row.ID)
	if err != nil {
		return nil, err
	}
	return UpdateBackupTarget200JSONResponse(toAPIBackupTarget(*fresh, nil)), nil
}

func (s *Server) DeleteBackupTarget(ctx context.Context, req DeleteBackupTargetRequestObject) (DeleteBackupTargetResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResBackups, rbac.LevelDelete) {
		s.deniedAudit(ctx, id, "backup.target_delete", ResBackups)
		return DeleteBackupTarget403JSONResponse{forbidden()}, nil
	}
	row, err := s.store.GetBackupTarget(ctx, req.Id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return DeleteBackupTarget404JSONResponse{notFoundErr("backup target")}, nil
		}
		return nil, err
	}
	if err := s.store.DeleteBackupTarget(ctx, row.ID); err != nil {
		return nil, err
	}
	if err := s.vault.DeleteScope(ctx, "target:"+row.ID.String()); err != nil {
		s.logger.Error("delete target secrets", "error", err)
	}
	s.audit.Log(ctx, actorFor(id), "backup.target_delete", ResBackups, "success", map[string]any{"name": row.Name})
	return DeleteBackupTarget204Response{}, nil
}

func (s *Server) TestBackupTarget(ctx context.Context, req TestBackupTargetRequestObject) (TestBackupTargetResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResBackups, rbac.LevelConfigure) {
		return TestBackupTarget403JSONResponse{forbidden()}, nil
	}
	row, err := s.store.GetBackupTarget(ctx, req.Id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return TestBackupTarget404JSONResponse{notFoundErr("backup target")}, nil
		}
		return nil, err
	}
	testCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	target, err := s.backups.OpenTarget(testCtx, row)
	if err == nil {
		err = target.Test(testCtx)
		target.Close()
	}
	if err != nil {
		_ = s.store.SetBackupTargetStatus(ctx, row.ID, "error", err.Error())
		return TestBackupTarget200JSONResponse(TestConnectionResult{Ok: false, Message: err.Error()}), nil
	}
	_ = s.store.SetBackupTargetStatus(ctx, row.ID, "ok", "")
	return TestBackupTarget200JSONResponse(TestConnectionResult{Ok: true, Message: "write test succeeded — the target is usable"}), nil
}

func (s *Server) RunBackupNow(ctx context.Context, req RunBackupNowRequestObject) (RunBackupNowResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResBackups, rbac.LevelControl) {
		s.deniedAudit(ctx, id, "backup.run", ResBackups)
		return RunBackupNow403JSONResponse{forbidden()}, nil
	}
	if _, err := s.store.GetBackupTarget(ctx, req.Id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return RunBackupNow404JSONResponse{notFoundErr("backup target")}, nil
		}
		return nil, err
	}
	job, err := s.jobs.Enqueue(ctx, "backup.app", backup.JobPayload{TargetID: req.Id, UserID: id.User.ID})
	if err != nil {
		return nil, err
	}
	s.audit.Log(ctx, actorFor(id), "backup.run", ResBackups, "success", map[string]any{"target": req.Id, "job": job.ID})
	return RunBackupNow202JSONResponse{JobQueuedJSONResponse{JobId: job.ID}}, nil
}

func (s *Server) ListBackupArchives(ctx context.Context, req ListBackupArchivesRequestObject) (ListBackupArchivesResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResBackups, rbac.LevelView) {
		return ListBackupArchives403JSONResponse{forbidden()}, nil
	}
	row, err := s.store.GetBackupTarget(ctx, req.Id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ListBackupArchives404JSONResponse{notFoundErr("backup target")}, nil
		}
		return nil, err
	}
	listCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	target, err := s.backups.OpenTarget(listCtx, row)
	if err != nil {
		return ListBackupArchives502JSONResponse{UpstreamErrorJSONResponse(errBody("unreachable", err.Error()))}, nil
	}
	defer target.Close()
	entries, err := target.List(listCtx, "homehub/")
	if err != nil {
		return ListBackupArchives502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	out := []BackupArchive{}
	for _, entry := range entries {
		out = append(out, BackupArchive{
			Path:      entry.Path,
			SizeBytes: entry.Size,
			ModTime:   entry.ModTime,
			Encrypted: strings.HasSuffix(entry.Path, ".age"),
		})
	}
	return ListBackupArchives200JSONResponse(out), nil
}

func (s *Server) InspectBackupArchive(ctx context.Context, req InspectBackupArchiveRequestObject) (InspectBackupArchiveResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResBackups, rbac.LevelView) {
		return InspectBackupArchive403JSONResponse{forbidden()}, nil
	}
	row, err := s.store.GetBackupTarget(ctx, req.Id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return InspectBackupArchive404JSONResponse{notFoundErr("backup target")}, nil
		}
		return nil, err
	}
	inspectCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	manifest, err := s.backups.InspectArchive(inspectCtx, row, req.Body.Path)
	if err != nil {
		return InspectBackupArchive400JSONResponse{BadRequestJSONResponse(errBody("unreadable", err.Error()))}, nil
	}
	return InspectBackupArchive200JSONResponse(BackupManifest{
		Version:    manifest.Version,
		CreatedAt:  manifest.CreatedAt,
		Kind:       manifest.Kind,
		DumpFormat: manifest.DumpFormat,
		Encrypted:  manifest.Encrypted,
	}), nil
}

func (s *Server) RestoreBackup(ctx context.Context, req RestoreBackupRequestObject) (RestoreBackupResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResBackups, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "backup.restore", ResBackups)
		return RestoreBackup403JSONResponse{forbidden()}, nil
	}
	if !req.Body.Confirm {
		return RestoreBackup400JSONResponse{BadRequestJSONResponse(errBody("not_confirmed", "restoring replaces the current database — set confirm to proceed"))}, nil
	}
	if _, err := s.store.GetBackupTarget(ctx, req.Body.TargetId); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return RestoreBackup404JSONResponse{notFoundErr("backup target")}, nil
		}
		return nil, err
	}
	job, err := s.jobs.Enqueue(ctx, "backup.restore", backup.JobPayload{
		TargetID: req.Body.TargetId, UserID: id.User.ID, Path: req.Body.Path,
	})
	if err != nil {
		return nil, err
	}
	s.audit.Log(ctx, actorFor(id), "backup.restore", ResBackups, "success",
		map[string]any{"target": req.Body.TargetId, "archive": req.Body.Path, "job": job.ID})
	return RestoreBackup202JSONResponse{JobQueuedJSONResponse{JobId: job.ID}}, nil
}

func (s *Server) ListBackupRuns(ctx context.Context, req ListBackupRunsRequestObject) (ListBackupRunsResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResBackups, rbac.LevelView) {
		return ListBackupRuns403JSONResponse{forbidden()}, nil
	}
	limit := 100
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	runs, err := s.store.ListBackupRuns(ctx, limit)
	if err != nil {
		return nil, err
	}
	out := []BackupRun{}
	for _, run := range runs {
		out = append(out, BackupRun{
			Id:         run.ID,
			TargetId:   run.TargetID,
			TargetName: run.TargetName,
			Kind:       BackupRunKind(run.Kind),
			Status:     BackupRunStatus(run.Status),
			FilePath:   run.FilePath,
			SizeBytes:  run.SizeBytes,
			Error:      run.Error,
			StartedAt:  run.StartedAt,
			FinishedAt: run.FinishedAt,
		})
	}
	return ListBackupRuns200JSONResponse(out), nil
}

// ---- schedules -------------------------------------------------------------

func (s *Server) backupScheduleFromRow(ctx context.Context, row store.Schedule) BackupSchedule {
	var payload backup.JobPayload
	_ = json.Unmarshal(row.Payload, &payload)
	name := ""
	if target, err := s.store.GetBackupTarget(ctx, payload.TargetID); err == nil {
		name = target.Name
	}
	return BackupSchedule{
		Id:         row.ID,
		TargetId:   payload.TargetID,
		TargetName: name,
		Cron:       row.CronExpr,
		Enabled:    row.Enabled,
		LastRunAt:  row.LastRunAt,
		NextRunAt:  row.NextRunAt,
	}
}

func (s *Server) ListBackupSchedules(ctx context.Context, _ ListBackupSchedulesRequestObject) (ListBackupSchedulesResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResBackups, rbac.LevelView) {
		return ListBackupSchedules403JSONResponse{forbidden()}, nil
	}
	rows, err := s.store.ListSchedulesByKind(ctx, "backup.app")
	if err != nil {
		return nil, err
	}
	out := []BackupSchedule{}
	for _, row := range rows {
		out = append(out, s.backupScheduleFromRow(ctx, row))
	}
	return ListBackupSchedules200JSONResponse(out), nil
}

func (s *Server) CreateBackupSchedule(ctx context.Context, req CreateBackupScheduleRequestObject) (CreateBackupScheduleResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResBackups, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "backup.schedule_create", ResBackups)
		return CreateBackupSchedule403JSONResponse{forbidden()}, nil
	}
	target, err := s.store.GetBackupTarget(ctx, req.Body.TargetId)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return CreateBackupSchedule404JSONResponse{notFoundErr("backup target")}, nil
		}
		return nil, err
	}
	next, err := jobs.NextRun(req.Body.Cron, time.Now())
	if err != nil {
		return CreateBackupSchedule400JSONResponse{BadRequestJSONResponse(errBody("invalid_cron", "not a valid cron expression: "+err.Error()))}, nil
	}
	row, err := s.store.CreateSchedule(ctx, "Backup to "+target.Name, "backup.app",
		backup.JobPayload{TargetID: target.ID}, req.Body.Cron, next)
	if err != nil {
		return nil, err
	}
	s.audit.Log(ctx, actorFor(id), "backup.schedule_create", ResBackups, "success",
		map[string]any{"target": target.Name, "cron": req.Body.Cron})
	return CreateBackupSchedule201JSONResponse(s.backupScheduleFromRow(ctx, *row)), nil
}

func (s *Server) UpdateBackupSchedule(ctx context.Context, req UpdateBackupScheduleRequestObject) (UpdateBackupScheduleResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResBackups, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "backup.schedule_update", ResBackups)
		return UpdateBackupSchedule403JSONResponse{forbidden()}, nil
	}
	row, err := s.store.GetSchedule(ctx, req.Id)
	if err != nil || row.Kind != "backup.app" {
		return UpdateBackupSchedule404JSONResponse{notFoundErr("schedule")}, nil
	}
	next, err := jobs.NextRun(req.Body.Cron, time.Now())
	if err != nil {
		return UpdateBackupSchedule400JSONResponse{BadRequestJSONResponse(errBody("invalid_cron", "not a valid cron expression: "+err.Error()))}, nil
	}
	if err := s.store.UpdateSchedule(ctx, row.ID, req.Body.Cron, req.Body.Enabled, next); err != nil {
		return nil, err
	}
	s.audit.Log(ctx, actorFor(id), "backup.schedule_update", ResBackups, "success",
		map[string]any{"cron": req.Body.Cron, "enabled": req.Body.Enabled})
	fresh, err := s.store.GetSchedule(ctx, row.ID)
	if err != nil {
		return nil, err
	}
	return UpdateBackupSchedule200JSONResponse(s.backupScheduleFromRow(ctx, *fresh)), nil
}

func (s *Server) DeleteBackupSchedule(ctx context.Context, req DeleteBackupScheduleRequestObject) (DeleteBackupScheduleResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResBackups, rbac.LevelConfigure) {
		return DeleteBackupSchedule403JSONResponse{forbidden()}, nil
	}
	row, err := s.store.GetSchedule(ctx, req.Id)
	if err != nil || row.Kind != "backup.app" {
		return DeleteBackupSchedule404JSONResponse{notFoundErr("schedule")}, nil
	}
	if err := s.store.DeleteSchedule(ctx, row.ID); err != nil {
		return nil, err
	}
	s.audit.Log(ctx, actorFor(id), "backup.schedule_delete", ResBackups, "success", map[string]any{"name": row.Name})
	return DeleteBackupSchedule204Response{}, nil
}

func (s *Server) GetBackupEncryptionKey(ctx context.Context, _ GetBackupEncryptionKeyRequestObject) (GetBackupEncryptionKeyResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResBackups, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "backup.export_key", ResBackups)
		return GetBackupEncryptionKey403JSONResponse{forbidden()}, nil
	}
	key, err := s.backups.ExportEncryptionKey(ctx)
	if err != nil {
		return nil, err
	}
	s.audit.Log(ctx, actorFor(id), "backup.export_key", ResBackups, "success", nil)
	return GetBackupEncryptionKey200JSONResponse{Key: key}, nil
}

// uuid referenced for schedule payloads.
var _ uuid.UUID
