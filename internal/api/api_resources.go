package api

import (
	"context"
	"errors"
	"strings"
	"time"

	"homehub/internal/httpctx"
	"homehub/internal/rbac"
	"homehub/internal/store"
)

// ---- SSH credentials (per resource) ---------------------------------------

func (s *Server) GetSSHCredential(ctx context.Context, req GetSSHCredentialRequestObject) (GetSSHCredentialResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, req.Params.Resource, rbac.LevelView) {
		return GetSSHCredential403JSONResponse{forbidden()}, nil
	}
	cred, err := s.store.GetSSHCredential(ctx, req.Params.Resource)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return GetSSHCredential404JSONResponse{notFoundErr("SSH credential")}, nil
		}
		return nil, err
	}
	return GetSSHCredential200JSONResponse(s.sshCredentialWithHostName(ctx, cred)), nil
}

func (s *Server) PutSSHCredential(ctx context.Context, req PutSSHCredentialRequestObject) (PutSSHCredentialResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	resource := strings.TrimSpace(req.Body.Resource)
	if !s.can(ctx, id, resource, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "resource.ssh_credential", resource)
		return PutSSHCredential403JSONResponse{forbidden()}, nil
	}
	if !rbac.ValidSelector(resource) || strings.Contains(resource, "*") {
		return PutSSHCredential400JSONResponse{BadRequestJSONResponse(errBody("invalid_resource", "resource must be a concrete path"))}, nil
	}
	// Binding to a central SSH host is the primary path: the registry entry
	// carries host/user/secret, so none of the inline fields are required.
	if req.Body.HostId != nil {
		h, herr := s.store.GetSSHHost(ctx, *req.Body.HostId)
		if herr != nil {
			return PutSSHCredential400JSONResponse{BadRequestJSONResponse(errBody("unknown_host", "that SSH host does not exist"))}, nil
		}
		// Binding hands this resource's shell the host's stored key, so it needs
		// the right to a shell on that host too — configure on one VM must not
		// borrow the Proxmox node's root login.
		if !s.canShellOnSSHHost(ctx, id, h.ID) {
			s.deniedAudit(ctx, id, "resource.ssh_credential", sshHostResource(h.ID))
			return PutSSHCredential403JSONResponse{forbidden()}, nil
		}
		if err := s.store.UpsertSSHCredential(ctx, &store.SSHCredential{
			Resource: resource, Host: h.Hostname, Port: h.Port, Username: h.Username,
			AuthType: h.AuthType, HostID: &h.ID,
		}); err != nil {
			return nil, err
		}
		s.audit.Log(ctx, actorFor(id), "resource.ssh_credential", resource, "success",
			map[string]any{"boundTo": h.Name})
		cred, cerr := s.store.GetSSHCredential(ctx, resource)
		if cerr != nil {
			return nil, cerr
		}
		return PutSSHCredential200JSONResponse(s.sshCredentialWithHostName(ctx, cred)), nil
	}

	host := ""
	if req.Body.Host != nil {
		host = strings.TrimSpace(*req.Body.Host)
	}
	if host == "" {
		return PutSSHCredential400JSONResponse{BadRequestJSONResponse(errBody("host_required", "a hostname or IP is required"))}, nil
	}
	port := 22
	if req.Body.Port != nil {
		port = *req.Body.Port
	}

	// A fresh credential needs a secret; an update may keep the stored one —
	// but only while it still goes to the same login. Re-pointing the host,
	// port or user without re-entering the secret would deliver the stored
	// password to whatever address the caller typed.
	keepsStored := false
	if prev, err := s.store.GetSSHCredential(ctx, resource); err == nil {
		keepsStored = prev.HostID == nil && prev.Host == host && prev.Port == port &&
			prev.Username == req.Body.Username && prev.AuthType == string(req.Body.AuthType)
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	secret := ""
	if req.Body.Secret != nil {
		secret = *req.Body.Secret
	}
	if secret == "" && !keepsStored {
		return PutSSHCredential400JSONResponse{BadRequestJSONResponse(errBody("secret_required", "a password or private key is required — re-enter it when the host, port or user changes"))}, nil
	}

	if err := s.store.UpsertSSHCredential(ctx, &store.SSHCredential{
		Resource: resource,
		Host:     host,
		Port:     port,
		Username: req.Body.Username,
		AuthType: string(req.Body.AuthType),
	}); err != nil {
		return nil, err
	}
	if secret != "" {
		if err := s.vault.Seal(ctx, "ssh", resource, []byte(secret)); err != nil {
			return nil, err
		}
	}
	s.audit.Log(ctx, actorFor(id), "resource.ssh_credential", resource, "success",
		map[string]any{"username": req.Body.Username, "authType": req.Body.AuthType})

	cred, err := s.store.GetSSHCredential(ctx, resource)
	if err != nil {
		return nil, err
	}
	return PutSSHCredential200JSONResponse(toAPISSHCredential(cred)), nil
}

func (s *Server) DeleteSSHCredential(ctx context.Context, req DeleteSSHCredentialRequestObject) (DeleteSSHCredentialResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	resource := req.Params.Resource
	if !s.can(ctx, id, resource, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "resource.ssh_credential_delete", resource)
		return DeleteSSHCredential403JSONResponse{forbidden()}, nil
	}
	if err := s.store.DeleteSSHCredential(ctx, resource); err != nil {
		return nil, err
	}
	if err := s.vault.Delete(ctx, "ssh", resource); err != nil {
		s.logger.Error("delete ssh secret", "resource", resource, "error", err)
	}
	s.audit.Log(ctx, actorFor(id), "resource.ssh_credential_delete", resource, "success", nil)
	return DeleteSSHCredential204Response{}, nil
}

func toAPISSHCredential(cred *store.SSHCredential) SSHCredentialInfo {
	out := SSHCredentialInfo{
		Resource:  cred.Resource,
		Host:      cred.Host,
		Port:      cred.Port,
		Username:  cred.Username,
		AuthType:  SSHCredentialInfoAuthType(cred.AuthType),
		UpdatedAt: cred.UpdatedAt,
	}
	out.HostId = cred.HostID
	return out
}

// sshCredentialWithHostName decorates the mapper with the bound registry
// host's display name, so the UI can show "via <name>" without a second call.
func (s *Server) sshCredentialWithHostName(ctx context.Context, cred *store.SSHCredential) SSHCredentialInfo {
	out := toAPISSHCredential(cred)
	if cred.HostID != nil {
		if h, err := s.store.GetSSHHost(ctx, *cred.HostID); err == nil {
			out.HostName = &h.Name
			// Surface the effective target so callers don't render blanks.
			out.Host, out.Port, out.Username = h.Hostname, h.Port, h.Username
			out.AuthType = SSHCredentialInfoAuthType(h.AuthType)
		}
	}
	return out
}

// ---- metrics ---------------------------------------------------------------

func (s *Server) QueryMetrics(ctx context.Context, req QueryMetricsRequestObject) (QueryMetricsResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, req.Params.Resource, rbac.LevelView) {
		return QueryMetrics403JSONResponse{forbidden()}, nil
	}
	points, err := s.metrics.Query(ctx, req.Params.Resource, req.Params.Metric, req.Params.From, req.Params.To)
	if err != nil {
		return nil, err
	}
	type pointRow = struct {
		Avg float32   `json:"avg"`
		Max float32   `json:"max"`
		Min float32   `json:"min"`
		Ts  time.Time `json:"ts"`
	}
	out := QueryMetrics200JSONResponse{Points: make([]pointRow, 0, len(points))}
	for _, p := range points {
		out.Points = append(out.Points, pointRow{
			Avg: float32(p.Avg), Max: float32(p.Max), Min: float32(p.Min), Ts: p.TS,
		})
	}
	return out, nil
}
