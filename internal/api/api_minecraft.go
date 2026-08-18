package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/TheOriUHD/homehub/internal/httpctx"
	"github.com/TheOriUHD/homehub/internal/integrations/minecraft"
	"github.com/TheOriUHD/homehub/internal/rbac"
	"github.com/TheOriUHD/homehub/internal/store"
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

// mcServer resolves a server within a network, enforcing per-server
// visibility (minecraft/<net>/server/<sid>, hidden-not-disabled).
func (s *Server) mcServer(ctx context.Context, id *httpctx.Identity, netID, serverID uuid.UUID) (*store.MinecraftServer, string, error) {
	server, err := s.store.GetMinecraftServer(ctx, serverID)
	if err != nil || server.IntegrationID != netID {
		return nil, "", store.ErrNotFound
	}
	resource := minecraft.TypeID + "/" + netID.String() + "/server/" + serverID.String()
	if !s.visibleUnder(ctx, id, resource) {
		return nil, "", store.ErrNotFound
	}
	return server, resource, nil
}

// mcSSHConfig assembles the network's SSH credential from config + vault.
func (s *Server) mcSSHConfig(ctx context.Context, row *store.Integration) minecraft.SSHConfig {
	var cfg struct {
		SSHHost string `json:"sshHost"`
		SSHPort int    `json:"sshPort"`
		SSHUser string `json:"sshUser"`
	}
	_ = json.Unmarshal(row.Config, &cfg)
	out := minecraft.SSHConfig{Host: cfg.SSHHost, Port: cfg.SSHPort, User: cfg.SSHUser}
	scope := "integration:" + row.ID.String()
	if raw, err := s.vault.Open(ctx, scope, "sshPassword"); err == nil {
		out.Password = string(raw)
	}
	if raw, err := s.vault.Open(ctx, scope, "sshKey"); err == nil {
		out.Key = string(raw)
	}
	return out
}

func (s *Server) McOverview(ctx context.Context, req McOverviewRequestObject) (McOverviewResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	row, resource, err := s.mcInstance(ctx, id, req.Id)
	if err != nil {
		return McOverview404JSONResponse{notFoundErr("instance")}, nil
	}

	out := McOverview{Servers: []McServer{}}
	ssh := s.mcSSHConfig(ctx, row)
	out.SshConfigured = ssh.Host != "" && ssh.User != "" && (ssh.Password != "" || ssh.Key != "")

	servers, err := s.store.ListMinecraftServers(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	for _, server := range servers {
		serverRes := resource + "/server/" + server.ID.String()
		if !s.visibleUnder(ctx, id, serverRes) {
			continue
		}
		entry := McServer{Id: server.ID, Label: server.Label}
		if server.ServerName != "" {
			v := server.ServerName
			entry.ServerName = &v
		}
		if server.Loader != "" {
			v := server.Loader
			entry.Loader = &v
		}
		if server.McVersion != "" {
			v := server.McVersion
			entry.McVersion = &v
		}
		if server.CompanionVersion != "" {
			v := server.CompanionVersion
			entry.CompanionVersion = &v
		}
		entry.PairedAt = server.PairedAt
		entry.LastConnectedAt = server.LastConnectedAt
		if server.LastRemoteIP != "" {
			v := server.LastRemoteIP
			entry.RemoteIp = &v
		}
		if sess, ok := s.mc.Session(server.ID); ok {
			entry.Connected = true
			if st := sess.Status(); st != nil {
				entry.Status = toAPIMcStatus(st)
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
			entry.Players = &list
		}
		out.Servers = append(out.Servers, entry)
	}

	// The pairing code is a credential: configure-level callers only.
	if s.can(ctx, id, resource, rbac.LevelConfigure) {
		if code, label, expires, _ := s.store.PendingMinecraftPairing(ctx, req.Id); code != "" && expires != nil {
			pending := McPairingCode{Code: code, ExpiresAt: *expires}
			if label != "" {
				pending.Label = &label
			}
			out.PendingPairing = &pending
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
	label := ""
	if req.Body != nil && req.Body.Label != nil {
		label = strings.TrimSpace(*req.Body.Label)
	}
	code, expires, err := s.store.CreateMinecraftPairing(ctx, req.Id, label, 15*time.Minute)
	if err != nil {
		return nil, err
	}
	s.audit.Log(ctx, actorFor(id), "mc.pairing_code", resource, "success", map[string]any{"label": label})
	resp := McPairingCode{Code: code, ExpiresAt: expires}
	if label != "" {
		resp.Label = &label
	}
	return McCreatePairingCode200JSONResponse(resp), nil
}

func (s *Server) McUpdateServer(ctx context.Context, req McUpdateServerRequestObject) (McUpdateServerResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	_, resource, err := s.mcInstance(ctx, id, req.Id)
	if err != nil {
		return McUpdateServer404JSONResponse{notFoundErr("instance")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "mc.server_update", resource)
		return McUpdateServer403JSONResponse{forbidden()}, nil
	}
	server, _, err := s.mcServer(ctx, id, req.Id, req.ServerId)
	if err != nil {
		return McUpdateServer404JSONResponse{notFoundErr("server")}, nil
	}

	if req.Body.Label != nil {
		label := strings.TrimSpace(*req.Body.Label)
		if label == "" {
			return McUpdateServer400JSONResponse{BadRequestJSONResponse(errBody("invalid_label", "the name cannot be empty"))}, nil
		}
		if err := s.store.RenameMinecraftServer(ctx, server.ID, label); err != nil {
			return nil, err
		}
		s.audit.Log(ctx, actorFor(id), "mc.server_rename", resource, "success",
			map[string]any{"serverId": server.ID.String(), "from": server.Label, "to": label})
	}

	if req.Body.NetworkId != nil && *req.Body.NetworkId != req.Id {
		targetID := *req.Body.NetworkId
		target, err := s.store.GetIntegration(ctx, targetID)
		if err != nil || target.Type != minecraft.TypeID {
			return McUpdateServer400JSONResponse{BadRequestJSONResponse(errBody("invalid_target", "the destination is not a Minecraft network"))}, nil
		}
		targetRes := target.Type + "/" + targetID.String()
		if !s.can(ctx, id, targetRes, rbac.LevelConfigure) {
			s.deniedAudit(ctx, id, "mc.server_move", targetRes)
			return McUpdateServer403JSONResponse{forbidden()}, nil
		}
		// Reseal the companion token under the destination network's scope
		// so the move never breaks the pairing. Wrapped pre-network servers
		// keep theirs in the legacy slot.
		oldScope := "integration:" + req.Id.String()
		token, err := s.vault.Open(ctx, oldScope, "agent-token-"+server.ID.String())
		if err != nil && server.ID == server.IntegrationID {
			token, err = s.vault.Open(ctx, oldScope, "agent-token")
		}
		if err != nil {
			return McUpdateServer400JSONResponse{BadRequestJSONResponse(errBody("no_token", "this server has no stored companion token — re-pair it instead"))}, nil
		}
		if err := s.vault.Seal(ctx, "integration:"+targetID.String(), "agent-token-"+server.ID.String(), token); err != nil {
			return nil, err
		}
		if err := s.store.MoveMinecraftServer(ctx, server.ID, targetID); err != nil {
			return nil, err
		}
		s.audit.Log(ctx, actorFor(id), "mc.server_move", resource, "success",
			map[string]any{"serverId": server.ID.String(), "to": targetID.String()})
		// Both networks' health changed shape immediately.
		s.integrations.Kick(req.Id)
		s.integrations.Kick(targetID)
	}
	return McUpdateServer204Response{}, nil
}

func (s *Server) McDeleteServer(ctx context.Context, req McDeleteServerRequestObject) (McDeleteServerResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	_, resource, err := s.mcInstance(ctx, id, req.Id)
	if err != nil {
		return McDeleteServer404JSONResponse{notFoundErr("instance")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "mc.server_delete", resource)
		return McDeleteServer403JSONResponse{forbidden()}, nil
	}
	server, _, err := s.mcServer(ctx, id, req.Id, req.ServerId)
	if err != nil {
		return McDeleteServer404JSONResponse{notFoundErr("server")}, nil
	}
	if err := s.store.DeleteMinecraftServer(ctx, req.Id, req.ServerId); err != nil {
		return McDeleteServer404JSONResponse{notFoundErr("server")}, nil
	}
	s.audit.Log(ctx, actorFor(id), "mc.server_delete", resource, "success",
		map[string]any{"serverId": req.ServerId.String(), "label": server.Label})
	return McDeleteServer204Response{}, nil
}

// mcStopVerb picks the graceful-shutdown console word for a loader:
// Velocity's proxy console uses "end"/"shutdown", everything else "stop".
func mcStopVerb(loader string) string {
	if loader == "velocity" {
		return "end"
	}
	return "stop"
}

func (s *Server) McRestartServer(ctx context.Context, req McRestartServerRequestObject) (McRestartServerResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	row, _, err := s.mcInstance(ctx, id, req.Id)
	if err != nil {
		return McRestartServer404JSONResponse{notFoundErr("instance")}, nil
	}
	server, resource, err := s.mcServer(ctx, id, req.Id, req.ServerId)
	if err != nil {
		return McRestartServer404JSONResponse{notFoundErr("server")}, nil
	}
	// A full restart is disruptive: control on the server, like commands.
	if !s.can(ctx, id, resource, rbac.LevelControl) {
		s.deniedAudit(ctx, id, "mc.restart", resource)
		return McRestartServer403JSONResponse{forbidden()}, nil
	}

	cfg := s.mcSSHConfig(ctx, row)
	if cfg.Host == "" || cfg.User == "" {
		return McRestartServer400JSONResponse{BadRequestJSONResponse(errBody("no_ssh", "this network has no SSH access configured — a full restart needs it"))}, nil
	}

	// The screen session and directory follow the operator's convention:
	// screen name == server folder == the label, under a configurable base
	// (default /home/minecraft/server).
	var netCfg struct {
		ServerDir string `json:"sshServerDir"`
	}
	_ = json.Unmarshal(row.Config, &netCfg)
	base := strings.TrimRight(netCfg.ServerDir, "/")
	if base == "" {
		base = "/home/minecraft/server"
	}
	screenName := server.Label
	dir := base + "/" + screenName
	script := minecraft.RestartScript(screenName, dir, mcStopVerb(server.Loader))

	rctx, cancel := context.WithTimeout(ctx, 130*time.Second)
	defer cancel()
	out, err := minecraft.RunSSH(rctx, cfg, script)
	message := strings.TrimSpace(out)
	ok := err == nil && strings.Contains(out, "RESTART_OK")
	outcome := "success"
	if !ok {
		outcome = "error"
		if message == "" && err != nil {
			message = err.Error()
		}
	}
	s.audit.Log(ctx, actorFor(id), "mc.restart", resource, outcome,
		map[string]any{"serverId": server.ID.String(), "label": server.Label})
	if !ok {
		return McRestartServer502JSONResponse{UpstreamErrorJSONResponse(errBody("restart_failed", message))}, nil
	}
	return McRestartServer200JSONResponse{Ok: true, Message: message}, nil
}

func (s *Server) McSshTest(ctx context.Context, req McSshTestRequestObject) (McSshTestResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	row, resource, err := s.mcInstance(ctx, id, req.Id)
	if err != nil {
		return McSshTest404JSONResponse{notFoundErr("instance")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "mc.ssh_test", resource)
		return McSshTest403JSONResponse{forbidden()}, nil
	}
	cfg := s.mcSSHConfig(ctx, row)
	tctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := minecraft.RunSSH(tctx, cfg, "echo homehub-ssh-ok && uname -a")
	outcome, message := "success", strings.TrimSpace(out)
	ok := err == nil && strings.Contains(out, "homehub-ssh-ok")
	if err != nil {
		outcome, message = "error", err.Error()
	} else if !ok {
		outcome, message = "error", "the command ran but did not produce the expected output"
	}
	s.audit.Log(ctx, actorFor(id), "mc.ssh_test", resource, outcome, nil)
	return McSshTest200JSONResponse{Ok: ok, Message: message}, nil
}

func (s *Server) McConsole(ctx context.Context, req McConsoleRequestObject) (McConsoleResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if _, _, err := s.mcInstance(ctx, id, req.Id); err != nil {
		return McConsole404JSONResponse{notFoundErr("instance")}, nil
	}
	if _, _, err := s.mcServer(ctx, id, req.Id, req.ServerId); err != nil {
		return McConsole404JSONResponse{notFoundErr("server")}, nil
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
	if sess, ok := s.mc.Session(req.ServerId); ok {
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
	if _, _, err := s.mcInstance(ctx, id, req.Id); err != nil {
		return McCommand404JSONResponse{notFoundErr("instance")}, nil
	}
	_, resource, err := s.mcServer(ctx, id, req.Id, req.ServerId)
	if err != nil {
		return McCommand404JSONResponse{notFoundErr("server")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelControl) {
		s.deniedAudit(ctx, id, "mc.command", resource)
		return McCommand403JSONResponse{forbidden()}, nil
	}
	command := strings.TrimSpace(req.Body.Command)
	if command == "" {
		return McCommand400JSONResponse{BadRequestJSONResponse(errBody("invalid_command", "command must not be empty"))}, nil
	}
	sess, ok := s.mc.Session(req.ServerId)
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
