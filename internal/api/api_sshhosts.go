package api

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"homehub/internal/httpctx"
	"homehub/internal/rbac"
	"homehub/internal/sshguard"
	"homehub/internal/store"
	"homehub/internal/term"
)

// The central SSH host registry. One named entry per machine, reused
// everywhere SSH is needed — Proxmox guest consoles, Minecraft networks,
// ad-hoc terminals, the assistant — so credentials are entered once and
// rotated in one place. Manual per-resource credentials still work; the
// registry is the default path, not the only one.

// ResSSHHosts is the management resource for the registry as a whole.
const ResSSHHosts = "system/ssh"

// sshHostResource is the per-host RBAC path, so a role can be granted one
// machine without the whole registry.
func sshHostResource(id uuid.UUID) string { return "ssh/" + id.String() }

// canShellOnSSHHost is the one bar for logging in as a registry host — a
// terminal, an assistant command, PatchPilot onboarding, or binding the host
// to another resource so that resource's shell opens with its key. Every one
// of them ends in a shell under that login, so they share rbac.LevelShell on
// the registry as a whole or on that one host.
func (s *Server) canShellOnSSHHost(ctx context.Context, id *httpctx.Identity, hostID uuid.UUID) bool {
	return s.can(ctx, id, ResSSHHosts, rbac.LevelShell) || s.can(ctx, id, sshHostResource(hostID), rbac.LevelShell)
}

// sshVaultScope holds each host's password or private key.
const sshVaultScope = store.SSHHostVaultScope

func (s *Server) toAPISSHHost(ctx context.Context, h *store.SSHHost) SSHHost {
	out := SSHHost{
		Id: h.ID, Name: h.Name, Hostname: h.Hostname, Port: h.Port,
		Username: h.Username, AuthType: SSHHostAuthType(h.AuthType), Notes: h.Notes,
		CreatedAt: h.CreatedAt, UpdatedAt: h.UpdatedAt,
	}
	if resources, err := s.store.ListResourcesForSSHHost(ctx, h.ID); err == nil {
		out.InUse = len(resources)
		out.BoundResources = &resources
	}
	return out
}

func (s *Server) ListSSHHosts(ctx context.Context, _ ListSSHHostsRequestObject) (ListSSHHostsResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	rows, err := s.store.ListSSHHosts(ctx)
	if err != nil {
		return nil, err
	}
	out := ListSSHHosts200JSONResponse{}
	for i := range rows {
		// Registry-wide view, or a grant on this one machine.
		if !s.can(ctx, id, ResSSHHosts, rbac.LevelView) &&
			!s.can(ctx, id, sshHostResource(rows[i].ID), rbac.LevelView) {
			continue
		}
		out = append(out, s.toAPISSHHost(ctx, &rows[i]))
	}
	return out, nil
}

// sshHostInput validates and normalizes the shared create/update body.
func sshHostInput(name, hostname, username, authType string, port *int) (store.SSHHost, string) {
	h := store.SSHHost{
		Name:     strings.TrimSpace(name),
		Hostname: strings.TrimSpace(hostname),
		Username: strings.TrimSpace(username),
		AuthType: authType,
		Port:     22,
	}
	if port != nil && *port > 0 {
		h.Port = *port
	}
	switch {
	case h.Name == "":
		return h, "a name is required"
	case h.Hostname == "":
		return h, "a hostname or IP is required"
	case h.Username == "":
		return h, "a username is required"
	case h.AuthType != "password" && h.AuthType != "key":
		return h, "auth type must be password or key"
	}
	return h, ""
}

func (s *Server) CreateSSHHost(ctx context.Context, req CreateSSHHostRequestObject) (CreateSSHHostResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResSSHHosts, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "ssh_host.create", ResSSHHosts)
		return CreateSSHHost403JSONResponse{forbidden()}, nil
	}
	h, bad := sshHostInput(req.Body.Name, req.Body.Hostname, req.Body.Username, string(req.Body.AuthType), req.Body.Port)
	if bad != "" {
		return CreateSSHHost400JSONResponse{BadRequestJSONResponse(errBody("bad_input", bad))}, nil
	}
	secret := ""
	if req.Body.Secret != nil {
		secret = *req.Body.Secret
	}
	if strings.TrimSpace(secret) == "" {
		return CreateSSHHost400JSONResponse{BadRequestJSONResponse(errBody("secret_required", "a password or private key is required"))}, nil
	}
	if req.Body.Notes != nil {
		h.Notes = strings.TrimSpace(*req.Body.Notes)
	}
	created, err := s.store.CreateSSHHost(ctx, &h)
	if err != nil {
		if store.IsUniqueViolation(err) {
			return CreateSSHHost409JSONResponse{ConflictJSONResponse(errBody("name_taken", "an SSH host with that name already exists"))}, nil
		}
		return nil, err
	}
	if err := s.vault.Seal(ctx, sshVaultScope, created.ID.String(), []byte(secret)); err != nil {
		// Don't leave a credential-less host behind.
		_ = s.store.DeleteSSHHost(ctx, created.ID)
		return nil, err
	}
	s.audit.Log(ctx, actorFor(id), "ssh_host.create", sshHostResource(created.ID), "success",
		map[string]any{"name": created.Name, "hostname": created.Hostname, "authType": created.AuthType})
	return CreateSSHHost201JSONResponse(s.toAPISSHHost(ctx, created)), nil
}

func (s *Server) UpdateSSHHost(ctx context.Context, req UpdateSSHHostRequestObject) (UpdateSSHHostResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResSSHHosts, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "ssh_host.update", sshHostResource(req.Id))
		return UpdateSSHHost403JSONResponse{forbidden()}, nil
	}
	prev, err := s.store.GetSSHHost(ctx, req.Id)
	if err != nil {
		return UpdateSSHHost404JSONResponse{notFoundErr("SSH host")}, nil
	}
	h, bad := sshHostInput(req.Body.Name, req.Body.Hostname, req.Body.Username, string(req.Body.AuthType), req.Body.Port)
	if bad != "" {
		return UpdateSSHHost400JSONResponse{BadRequestJSONResponse(errBody("bad_input", bad))}, nil
	}
	// The stored secret may only follow the login it was entered for. A
	// password is sent to the server in the clear (inside SSH), so re-pointing
	// a password host would hand it to whatever the new address is; switching
	// the auth type would present a private key as a password.
	newSecret := req.Body.Secret != nil && strings.TrimSpace(*req.Body.Secret) != ""
	moved := h.Hostname != prev.Hostname || h.Port != prev.Port || h.Username != prev.Username
	if !newSecret && (h.AuthType != prev.AuthType || (moved && h.AuthType == "password")) {
		return UpdateSSHHost400JSONResponse{BadRequestJSONResponse(errBody("secret_required",
			"enter the password or key again — a saved one is never sent to a changed address, login or auth type"))}, nil
	}
	h.ID = req.Id
	if req.Body.Notes != nil {
		h.Notes = strings.TrimSpace(*req.Body.Notes)
	}
	updated, err := s.store.UpdateSSHHost(ctx, &h)
	if err != nil {
		if store.IsUniqueViolation(err) {
			return UpdateSSHHost409JSONResponse{ConflictJSONResponse(errBody("name_taken", "an SSH host with that name already exists"))}, nil
		}
		return nil, err
	}
	// An empty secret keeps the stored one — the usual "leave blank" rule.
	if newSecret {
		if err := s.vault.Seal(ctx, sshVaultScope, req.Id.String(), []byte(*req.Body.Secret)); err != nil {
			return nil, err
		}
	}
	s.audit.Log(ctx, actorFor(id), "ssh_host.update", sshHostResource(req.Id), "success",
		map[string]any{"name": updated.Name, "hostname": updated.Hostname})
	return UpdateSSHHost200JSONResponse(s.toAPISSHHost(ctx, updated)), nil
}

func (s *Server) DeleteSSHHost(ctx context.Context, req DeleteSSHHostRequestObject) (DeleteSSHHostResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResSSHHosts, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "ssh_host.delete", sshHostResource(req.Id))
		return DeleteSSHHost403JSONResponse{forbidden()}, nil
	}
	if _, err := s.store.GetSSHHost(ctx, req.Id); err != nil {
		return DeleteSSHHost404JSONResponse{notFoundErr("SSH host")}, nil
	}
	if err := s.store.DeleteSSHHost(ctx, req.Id); err != nil {
		return nil, err
	}
	_ = s.vault.Delete(ctx, sshVaultScope, req.Id.String())
	s.audit.Log(ctx, actorFor(id), "ssh_host.delete", sshHostResource(req.Id), "success", nil)
	return DeleteSSHHost204Response{}, nil
}

// sshTargetForHost assembles a dialable target from a registry entry.
func (s *Server) sshTargetForHost(ctx context.Context, h *store.SSHHost) (term.SSHTarget, error) {
	secret, err := s.vault.Open(ctx, sshVaultScope, h.ID.String())
	if err != nil {
		return term.SSHTarget{}, errors.New("stored secret unreadable — re-enter the credentials for this host")
	}
	return term.SSHTarget{
		Host: h.Hostname, Port: h.Port, Username: h.Username,
		AuthType: h.AuthType, Secret: string(secret),
	}, nil
}

func (s *Server) TestSSHHost(ctx context.Context, req TestSSHHostRequestObject) (TestSSHHostResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	h, err := s.store.GetSSHHost(ctx, req.Id)
	if err != nil {
		return TestSSHHost404JSONResponse{notFoundErr("SSH host")}, nil
	}
	if !s.can(ctx, id, ResSSHHosts, rbac.LevelView) && !s.can(ctx, id, sshHostResource(req.Id), rbac.LevelView) {
		return TestSSHHost403JSONResponse{forbidden()}, nil
	}
	target, terr := s.sshTargetForHost(ctx, h)
	if terr != nil {
		return TestSSHHost200JSONResponse{Ok: false, Message: terr.Error()}, nil
	}
	tctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	who, rerr := term.TestSSH(tctx, target)
	if rerr != nil {
		return TestSSHHost200JSONResponse{Ok: false, Message: rerr.Error()}, nil
	}
	return TestSSHHost200JSONResponse{Ok: true, Message: "Connected — " + who}, nil
}

func (s *Server) OpenSSHHostTerminal(ctx context.Context, req OpenSSHHostTerminalRequestObject) (OpenSSHHostTerminalResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	h, err := s.store.GetSSHHost(ctx, req.Id)
	if err != nil {
		return OpenSSHHostTerminal404JSONResponse{notFoundErr("SSH host")}, nil
	}
	if !s.canShellOnSSHHost(ctx, id, req.Id) {
		s.deniedAudit(ctx, id, "ssh_host.terminal", sshHostResource(req.Id))
		return OpenSSHHostTerminal403JSONResponse{forbidden()}, nil
	}
	target, terr := s.sshTargetForHost(ctx, h)
	if terr != nil {
		return OpenSSHHostTerminal400JSONResponse{BadRequestJSONResponse(errBody("no_secret", terr.Error()))}, nil
	}
	sessionID := s.term.Create(id.User.ID, term.OpenSSH(target))
	s.audit.Log(ctx, actorFor(id), "ssh_host.terminal", sshHostResource(req.Id), "success",
		map[string]any{"name": h.Name})
	return OpenSSHHostTerminal200JSONResponse(TermSession{SessionId: sessionID, WsPath: "/api/term/" + sessionID}), nil
}

// ---- shared resolution -------------------------------------------------------

// ResolveSSHForResource returns a dialable target for a resource: the bound
// registry host when one is set, otherwise the resource's own inline
// credential. This is the single lookup every SSH consumer should use.
func (s *Server) ResolveSSHForResource(ctx context.Context, resource string) (term.SSHTarget, error) {
	cred, err := s.store.GetSSHCredential(ctx, resource)
	if err != nil {
		return term.SSHTarget{}, err
	}
	if cred.HostID != nil {
		h, herr := s.store.GetSSHHost(ctx, *cred.HostID)
		if herr != nil {
			return term.SSHTarget{}, errors.New("the SSH host this resource points at no longer exists — pick another")
		}
		return s.sshTargetForHost(ctx, h)
	}
	secret, verr := s.vault.Open(ctx, "ssh", resource)
	if verr != nil {
		return term.SSHTarget{}, errors.New("stored SSH secret unreadable — re-enter the credentials")
	}
	if cred.Host == "" {
		return term.SSHTarget{}, errors.New("no host or IP stored — set it in the SSH settings")
	}
	return term.SSHTarget{
		Host: cred.Host, Port: cred.Port, Username: cred.Username,
		AuthType: cred.AuthType, Secret: string(secret),
	}, nil
}

// ---- assistant bridge ---------------------------------------------------------

// sshAgentTimeout bounds an assistant-issued command; a shell that hangs must
// not hold the turn open forever.
const sshAgentTimeout = 45 * time.Second

// sshAgentOutputCap keeps a chatty command from flooding the transcript.
const sshAgentOutputCap = 16 << 10

// runSSHOnHost executes one command on a registry host for the assistant.
// Classification and permission checks happen in the tool layer; this is the
// execution path, and it re-checks the risk as a defence-in-depth backstop so
// no future caller can route a blocked command through it.
func (s *Server) runSSHOnHost(ctx context.Context, hostID, command string) (string, int, error) {
	if v := sshguard.Classify(command); v.Risk == sshguard.RiskBlocked {
		return "", 0, errors.New("refused: " + v.Reason)
	}
	id, err := uuid.Parse(hostID)
	if err != nil {
		return "", 0, errors.New("bad host id")
	}
	h, err := s.store.GetSSHHost(ctx, id)
	if err != nil {
		return "", 0, errors.New("that SSH host no longer exists")
	}
	target, terr := s.sshTargetForHost(ctx, h)
	if terr != nil {
		return "", 0, terr
	}
	rctx, cancel := context.WithTimeout(ctx, sshAgentTimeout)
	defer cancel()
	return term.RunSSH(rctx, target, command, sshAgentOutputCap)
}

// ProvisionSSHHost is the one-click path to key auth: enter the address, the
// user, and the password once — HomeHub generates a keypair, installs it,
// proves it works, and stores only the private key. The password is used for
// this call and then discarded.
func (s *Server) ProvisionSSHHost(ctx context.Context, req ProvisionSSHHostRequestObject) (ProvisionSSHHostResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResSSHHosts, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "ssh_host.provision", ResSSHHosts)
		return ProvisionSSHHost403JSONResponse{forbidden()}, nil
	}
	hostname := strings.TrimSpace(req.Body.Hostname)
	username := strings.TrimSpace(req.Body.Username)
	password := req.Body.Password
	if hostname == "" || username == "" || strings.TrimSpace(password) == "" {
		return ProvisionSSHHost400JSONResponse{BadRequestJSONResponse(errBody("bad_input", "address, username and password are required"))}, nil
	}
	port := 22
	if req.Body.Port != nil && *req.Body.Port > 0 {
		port = *req.Body.Port
	}
	name := hostname
	if req.Body.Name != nil && strings.TrimSpace(*req.Body.Name) != "" {
		name = strings.TrimSpace(*req.Body.Name)
	}
	notes := ""
	if req.Body.Notes != nil {
		notes = strings.TrimSpace(*req.Body.Notes)
	}

	kp, err := term.GenerateKeyPair("homehub@" + name)
	if err != nil {
		return nil, err
	}
	pctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	if err := term.InstallAuthorizedKey(pctx, term.SSHTarget{
		Host: hostname, Port: port, Username: username,
		AuthType: "password", Secret: password,
	}, kp); err != nil {
		s.audit.Log(ctx, actorFor(id), "ssh_host.provision", ResSSHHosts, "error",
			map[string]any{"hostname": hostname, "username": username, "error": err.Error()})
		return ProvisionSSHHost400JSONResponse{BadRequestJSONResponse(errBody("provision_failed", err.Error()))}, nil
	}

	// The key works — now persist. Convert in place when an id was given.
	rec := store.SSHHost{
		Name: name, Hostname: hostname, Port: port, Username: username,
		AuthType: "key", Notes: notes,
	}
	var saved *store.SSHHost
	if req.Body.Id != nil {
		existing, gerr := s.store.GetSSHHost(ctx, *req.Body.Id)
		if gerr != nil {
			return ProvisionSSHHost400JSONResponse{BadRequestJSONResponse(errBody("unknown_host", "that SSH host does not exist"))}, nil
		}
		rec.ID = existing.ID
		if req.Body.Name == nil || strings.TrimSpace(*req.Body.Name) == "" {
			rec.Name = existing.Name // keep the operator's chosen name
		}
		if notes == "" {
			rec.Notes = existing.Notes
		}
		saved, err = s.store.UpdateSSHHost(ctx, &rec)
	} else {
		saved, err = s.store.CreateSSHHost(ctx, &rec)
	}
	if err != nil {
		if store.IsUniqueViolation(err) {
			return ProvisionSSHHost409JSONResponse{ConflictJSONResponse(errBody("name_taken", "an SSH host with that name already exists"))}, nil
		}
		return nil, err
	}
	if err := s.vault.Seal(ctx, sshVaultScope, saved.ID.String(), []byte(kp.PrivatePEM)); err != nil {
		if req.Body.Id == nil {
			_ = s.store.DeleteSSHHost(ctx, saved.ID) // no credential-less leftovers
		}
		return nil, err
	}
	s.audit.Log(ctx, actorFor(id), "ssh_host.provision", sshHostResource(saved.ID), "success",
		map[string]any{"name": saved.Name, "hostname": hostname, "username": username})
	return ProvisionSSHHost200JSONResponse(s.toAPISSHHost(ctx, saved)), nil
}
