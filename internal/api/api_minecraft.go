package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"strings"
	"time"

	"github.com/google/uuid"

	"homehub/internal/httpctx"
	"homehub/internal/integrations/minecraft"
	"homehub/internal/rbac"
	"homehub/internal/store"
)

// mcInstance resolves an integration id to a Minecraft instance, enforcing
// visibility (invisible and non-Minecraft ids are both 404).
func (s *Server) mcInstance(ctx context.Context, id *httpctx.Identity, instID uuid.UUID) (*store.Integration, string, error) {
	row, err := s.store.GetIntegration(ctx, instID)
	if err != nil || row.Type != minecraft.TypeID {
		return nil, "", store.ErrNotFound
	}
	resource := row.Type + "/" + instID.String()
	if !s.visibleUnder(ctx, id, resource) {
		return nil, "", store.ErrNotFound
	}
	return row, resource, nil
}

func (s *Server) McOverview(ctx context.Context, req McOverviewRequestObject) (McOverviewResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	_, resource, err := s.mcInstance(ctx, id, req.Id)
	if err != nil {
		return McOverview404JSONResponse{notFoundErr("instance")}, nil
	}

	out := McOverview{}
	if meta, err := s.store.GetMinecraftServer(ctx, req.Id); err == nil {
		out.ServerName = &meta.ServerName
		out.Loader = &meta.Loader
		out.McVersion = &meta.McVersion
		out.CompanionVersion = &meta.CompanionVersion
		out.PairedAt = meta.PairedAt
		out.LastConnectedAt = meta.LastConnectedAt
		out.RemoteIp = &meta.LastRemoteIP
	}
	if sess, ok := s.mc.Session(req.Id); ok {
		out.Connected = true
		if st := sess.Status(); st != nil {
			out.Status = toAPIMcStatus(st)
		}
		players := sess.Players()
		list := make([]McPlayer, 0, len(players))
		for _, p := range players {
			player := McPlayer{Name: p.Name}
			if p.UUID != "" {
				u := p.UUID
				player.Uuid = &u
			}
			list = append(list, player)
		}
		out.Players = &list
	}
	// The pairing code is a credential: configure-level callers only.
	if s.can(ctx, id, resource, rbac.LevelConfigure) {
		if code, expires, _ := s.store.PendingMinecraftPairing(ctx, req.Id); code != "" && expires != nil {
			out.PendingPairing = &McPairingCode{Code: code, ExpiresAt: *expires}
		}
	}
	return McOverview200JSONResponse(out), nil
}

func toAPIMcStatus(st *minecraft.StatusSnapshot) *McStatus {
	out := &McStatus{
		PlayersOnline: st.PlayersOnline,
		PlayersMax:    st.PlayersMax,
	}
	if st.TPS > 0 {
		tps := st.TPS
		out.Tps = &tps
	}
	if st.MSPT > 0 {
		mspt := st.MSPT
		out.Mspt = &mspt
	}
	if st.HeapUsedMB > 0 {
		v := st.HeapUsedMB
		out.HeapUsedMb = &v
	}
	if st.HeapMaxMB > 0 {
		v := st.HeapMaxMB
		out.HeapMaxMb = &v
	}
	if len(st.Downstream) > 0 {
		ds := make([]struct {
			Name    string `json:"name"`
			Players int    `json:"players"`
		}, len(st.Downstream))
		for i, d := range st.Downstream {
			ds[i].Name = d.Name
			ds[i].Players = d.Players
		}
		out.Downstream = &ds
	}
	return out
}

func (s *Server) McCreatePairingCode(ctx context.Context, req McCreatePairingCodeRequestObject) (McCreatePairingCodeResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	_, resource, err := s.mcInstance(ctx, id, req.Id)
	if err != nil {
		return McCreatePairingCode404JSONResponse{notFoundErr("instance")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "mc.pairing_code", resource)
		return McCreatePairingCode403JSONResponse{forbidden()}, nil
	}
	code, expires, err := s.store.CreateMinecraftPairing(ctx, req.Id, 15*time.Minute)
	if err != nil {
		return nil, err
	}
	s.audit.Log(ctx, actorFor(id), "mc.pairing_code", resource, "success", nil)
	return McCreatePairingCode200JSONResponse(McPairingCode{Code: code, ExpiresAt: expires}), nil
}

func (s *Server) McConsole(ctx context.Context, req McConsoleRequestObject) (McConsoleResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if _, _, err := s.mcInstance(ctx, id, req.Id); err != nil {
		return McConsole404JSONResponse{notFoundErr("instance")}, nil
	}
	after := int64(-1)
	if req.Params.After != nil {
		after = *req.Params.After
	}
	limit := 1000
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	out := McConsole{Lines: []McConsoleLine{}}
	if sess, ok := s.mc.Session(req.Id); ok {
		out.Connected = true
		lines, oldest, next := sess.Console.After(after, limit)
		out.OldestSeq = oldest
		out.NextSeq = next
		for _, ln := range lines {
			line := McConsoleLine{Seq: ln.Seq, Ts: ln.TS, Text: ln.Text}
			if ln.Level != "" {
				lv := ln.Level
				line.Level = &lv
			}
			out.Lines = append(out.Lines, line)
		}
	}
	return McConsole200JSONResponse(out), nil
}

func (s *Server) McCommand(ctx context.Context, req McCommandRequestObject) (McCommandResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	_, resource, err := s.mcInstance(ctx, id, req.Id)
	if err != nil {
		return McCommand404JSONResponse{notFoundErr("instance")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelControl) {
		s.deniedAudit(ctx, id, "mc.command", resource)
		return McCommand403JSONResponse{forbidden()}, nil
	}
	command := strings.TrimSpace(req.Body.Command)
	if command == "" {
		return McCommand400JSONResponse{BadRequestJSONResponse(errBody("invalid_command", "command must not be empty"))}, nil
	}
	sess, ok := s.mc.Session(req.Id)
	if !ok {
		return McCommand502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", "companion not connected"))}, nil
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ok, message, err := sess.Command(cctx, command)
	if err != nil {
		s.audit.Log(ctx, actorFor(id), "mc.command", resource, "error",
			map[string]any{"command": command, "error": err.Error()})
		return McCommand502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	outcome := "success"
	if !ok {
		outcome = "error"
	}
	s.audit.Log(ctx, actorFor(id), "mc.command", resource, outcome, map[string]any{"command": command})
	resp := McCommand200JSONResponse{Ok: ok}
	if message != "" {
		resp.Message = &message
	}
	return resp, nil
}

// companionManifest is dist/manifest.json, written by the Gradle build.
type companionManifest []struct {
	Loader     string `json:"loader"`
	File       string `json:"file"`
	McVersions string `json:"mcVersions"`
}

func (s *Server) McDownloads(ctx context.Context, _ McDownloadsRequestObject) (McDownloadsResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	out := []McDownload{}
	if s.companion == nil {
		return McDownloads200JSONResponse(out), nil
	}
	raw, err := fs.ReadFile(s.companion, "manifest.json")
	if err != nil {
		return McDownloads200JSONResponse(out), nil
	}
	var manifest companionManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		s.logger.Error("companion manifest unreadable", "error", err)
		return McDownloads200JSONResponse(out), nil
	}
	for _, entry := range manifest {
		info, err := fs.Stat(s.companion, entry.File)
		if err != nil {
			continue
		}
		out = append(out, McDownload{
			Loader:     entry.Loader,
			File:       entry.File,
			SizeBytes:  info.Size(),
			McVersions: entry.McVersions,
		})
	}
	return McDownloads200JSONResponse(out), nil
}

func (s *Server) McDownloadFile(ctx context.Context, req McDownloadFileRequestObject) (McDownloadFileResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	// The generated path pattern already constrains to a bare *.jar name;
	// reject anything path-ish defensively anyway.
	if s.companion == nil || strings.ContainsAny(req.File, "/\\") {
		return McDownloadFile404JSONResponse{notFoundErr("file")}, nil
	}
	raw, err := fs.ReadFile(s.companion, req.File)
	if err != nil {
		return McDownloadFile404JSONResponse{notFoundErr("file")}, nil
	}
	httpctx.Writer(ctx).Header().Set("Content-Disposition", `attachment; filename="`+req.File+`"`)
	return McDownloadFile200ApplicationjavaArchiveResponse{
		Body:          bytes.NewReader(raw),
		ContentLength: int64(len(raw)),
	}, nil
}
