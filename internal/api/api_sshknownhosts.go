package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"homehub/internal/httpctx"
	"homehub/internal/rbac"
	"homehub/internal/term"
)

// Recovering from a changed SSH host key.
//
// Pinning is trust-on-first-use, and a changed key stops every connection to
// that address: the terminal, a Docker daemon over SSH, a Minecraft host, the
// agent's commands. That is the right default — a key changes because the
// machine was rebuilt, or because something is sitting in the middle, and
// nothing here can tell those apart. What was missing was the way back: the
// only cure was deleting the registry entry and typing the credentials in
// again, which fixes the pin by throwing away everything around it.
//
// So: look at both fingerprints, then accept the one the machine is presenting.
// Trusting re-probes and refuses if the live key is no longer the one being
// accepted, which closes the window between reading a screen and acting on it.
// The pin is keyed by address, so one decision restores every consumer at once.

// hostKeyProbeTimeout bounds a probe. A machine that is off answers nothing,
// and the operator should hear that quickly rather than watch a spinner.
const hostKeyProbeTimeout = 12 * time.Second

func (s *Server) ListSSHKnownHosts(ctx context.Context, _ ListSSHKnownHostsRequestObject) (ListSSHKnownHostsResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResSSHHosts, rbac.LevelView) {
		return ListSSHKnownHosts403JSONResponse{forbidden()}, nil
	}
	rows, err := s.store.ListSSHKnownHosts(ctx)
	if err != nil {
		return nil, err
	}
	users := s.sshAddressUsers(ctx)
	items := make([]SSHKnownHost, 0, len(rows))
	for _, r := range rows {
		items = append(items, SSHKnownHost{
			Host: r.Host, Port: r.Port, Fingerprint: r.Fingerprint,
			UpdatedAt: r.UpdatedAt, UsedBy: users[addrKey(r.Host, r.Port)],
		})
	}
	return ListSSHKnownHosts200JSONResponse{Items: items}, nil
}

func addrKey(host string, port int) string { return net.JoinHostPort(host, fmt.Sprint(port)) }

// sshAddressUsers maps an address to the things that connect to it, so a pin
// reads as "the Minecraft VM" rather than a bare address the operator has to
// recognise. Registry entries and the systems configured to dial over SSH;
// PatchPilot is deliberately absent because it pins per managed host of its
// own.
func (s *Server) sshAddressUsers(ctx context.Context) map[string][]string {
	out := map[string][]string{}
	add := func(host string, port int, name string) {
		if host = strings.TrimSpace(host); host == "" || name == "" {
			return
		}
		if port == 0 {
			port = 22
		}
		key := addrKey(host, port)
		for _, existing := range out[key] {
			if existing == name {
				return
			}
		}
		out[key] = append(out[key], name)
	}

	if hosts, err := s.store.ListSSHHosts(ctx); err == nil {
		for _, h := range hosts {
			add(h.Hostname, h.Port, h.Name)
		}
	}
	integrations, err := s.store.ListIntegrations(ctx)
	if err != nil {
		return out
	}
	for _, in := range integrations {
		var cfg struct {
			Host      string `json:"host"`
			Port      int    `json:"port"`
			Transport string `json:"transport"`
			SSHHost   string `json:"sshHost"`
			SSHPort   int    `json:"sshPort"`
		}
		if json.Unmarshal(in.Config, &cfg) != nil {
			continue
		}
		if cfg.Transport == "ssh" {
			add(cfg.Host, cfg.Port, in.Name)
		}
		add(cfg.SSHHost, cfg.SSHPort, in.Name)
	}
	return out
}

// checkHostKey reads what the address is presenting and compares it with the
// pin. Unreachable is reported as a status rather than an error: "the machine
// is off" and "the machine is lying about who it is" are different answers and
// must not look the same.
func (s *Server) checkHostKey(ctx context.Context, host string, port int) SSHHostKeyCheck {
	out := SSHHostKeyCheck{Host: host, Port: port}
	pinned, err := s.store.GetSSHHostKey(ctx, host, port)
	if err == nil && pinned != "" {
		out.Pinned = &pinned
	}

	pctx, cancel := context.WithTimeout(ctx, hostKeyProbeTimeout)
	defer cancel()
	observed, perr := term.ProbeHostKey(pctx, host, port)
	if perr != nil {
		out.Status = "unreachable"
		out.Message = ptr(perr.Error())
		return out
	}
	out.Observed = &observed

	// The message says what the status MEANS, never what it already says: the
	// screen puts the two next to each other, and a sentence repeated back at
	// the reader is a sentence they stop reading.
	switch {
	case pinned == "":
		out.Status = "unpinned"
		out.Message = ptr("The next connection will record whatever key it sees.")
	case pinned == observed:
		out.Status = "match"
		out.Message = ptr("Nothing here is blocking connections to this address.")
	default:
		out.Status = "changed"
		out.Message = ptr("Every connection to this address is being refused until it is resolved.")
	}
	return out
}

func (s *Server) CheckSSHHostKey(ctx context.Context, req CheckSSHHostKeyRequestObject) (CheckSSHHostKeyResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResSSHHosts, rbac.LevelView) {
		return CheckSSHHostKey403JSONResponse{forbidden()}, nil
	}
	host, port, err := hostKeyRef(req.Body.Host, req.Body.Port)
	if err != nil {
		return CheckSSHHostKey400JSONResponse{BadRequestJSONResponse(errBody("bad_input", err.Error()))}, nil
	}
	return CheckSSHHostKey200JSONResponse(s.checkHostKey(ctx, host, port)), nil
}

func (s *Server) TrustSSHHostKey(ctx context.Context, req TrustSSHHostKeyRequestObject) (TrustSSHHostKeyResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResSSHHosts, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "ssh_host.trust_key", ResSSHHosts)
		return TrustSSHHostKey403JSONResponse{forbidden()}, nil
	}
	host, port, err := hostKeyRef(req.Body.Host, req.Body.Port)
	if err != nil {
		return TrustSSHHostKey400JSONResponse{BadRequestJSONResponse(errBody("bad_input", err.Error()))}, nil
	}
	accepting := strings.TrimSpace(req.Body.Fingerprint)
	if accepting == "" {
		return TrustSSHHostKey400JSONResponse{BadRequestJSONResponse(errBody("bad_input",
			"say which fingerprint you are accepting — check the host first"))}, nil
	}

	// Re-probe rather than trusting what the caller sends. Between reading a
	// fingerprint and accepting it the machine may have changed again, and
	// this is the one call where pinning the wrong key matters.
	live := s.checkHostKey(ctx, host, port)
	if live.Status == "unreachable" {
		return TrustSSHHostKey409JSONResponse(errBody("unreachable",
			"cannot read a key from that address right now, so there is nothing to accept")), nil
	}
	if live.Observed == nil || *live.Observed != accepting {
		observed := "nothing"
		if live.Observed != nil {
			observed = *live.Observed
		}
		return TrustSSHHostKey409JSONResponse(errBody("key_moved", fmt.Sprintf(
			"that address is presenting %s, not the %s you are accepting — check it again",
			observed, accepting))), nil
	}
	if err := s.store.TrustSSHHostKey(ctx, host, port, accepting); err != nil {
		return nil, err
	}
	previous := ""
	if live.Pinned != nil {
		previous = *live.Pinned
	}
	s.audit.Log(ctx, actorFor(id), "ssh_host.trust_key", ResSSHHosts, "success",
		map[string]any{"address": addrKey(host, port), "previous": previous, "trusted": accepting})

	out := s.checkHostKey(ctx, host, port)
	if out.Status == "match" {
		out.Message = ptr("Connections to this address work again — the terminal, any system that dials it over SSH, and the assistant.")
	}
	return TrustSSHHostKey200JSONResponse(out), nil
}

func (s *Server) ForgetSSHHostKey(ctx context.Context, req ForgetSSHHostKeyRequestObject) (ForgetSSHHostKeyResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResSSHHosts, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "ssh_host.forget_key", ResSSHHosts)
		return ForgetSSHHostKey403JSONResponse{forbidden()}, nil
	}
	host, port, err := hostKeyRef(req.Body.Host, req.Body.Port)
	if err != nil {
		return ForgetSSHHostKey400JSONResponse{BadRequestJSONResponse(errBody("bad_input", err.Error()))}, nil
	}
	removed, err := s.store.ForgetSSHHostKey(ctx, host, port)
	if err != nil {
		return nil, err
	}
	if !removed {
		return ForgetSSHHostKey404JSONResponse{notFoundErr("pinned key")}, nil
	}
	s.audit.Log(ctx, actorFor(id), "ssh_host.forget_key", ResSSHHosts, "success",
		map[string]any{"address": addrKey(host, port)})
	return ForgetSSHHostKey204Response{}, nil
}

// hostKeyRef normalises an address from a request body.
func hostKeyRef(host string, port *int) (string, int, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return "", 0, errors.New("which host?")
	}
	p := 22
	if port != nil {
		p = *port
	}
	if p < 1 || p > 65535 {
		return "", 0, errors.New("port out of range")
	}
	return host, p, nil
}
