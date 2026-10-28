package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"

	"github.com/google/uuid"

	"homehub/internal/httpctx"
	"homehub/internal/integration"
	"homehub/internal/rbac"
	"homehub/internal/store"
)

func (s *Server) ListIntegrationTypes(ctx context.Context, _ ListIntegrationTypesRequestObject) (ListIntegrationTypesResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResIntegrations, rbac.LevelConfigure) {
		return ListIntegrationTypes403JSONResponse{forbidden()}, nil
	}
	out := []IntegrationType{}
	for _, d := range s.integrations.Drivers() {
		out = append(out, IntegrationType{
			Id:          d.TypeID(),
			Name:        d.DisplayName(),
			Description: d.Description(),
			SecretNames: d.SecretNames(),
		})
	}
	return ListIntegrationTypes200JSONResponse(out), nil
}

func (s *Server) ListIntegrations(ctx context.Context, _ ListIntegrationsRequestObject) (ListIntegrationsResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	rows, err := s.store.ListIntegrations(ctx)
	if err != nil {
		return nil, err
	}
	out := []Integration{}
	for _, row := range rows {
		if s.visibleUnder(ctx, id, row.Type+"/"+row.ID.String()) {
			out = append(out, toAPIIntegration(row))
		}
	}
	return ListIntegrations200JSONResponse(out), nil
}

// applySSHBindingForTest resolves a registry SSH host into the plain config
// and secret entries a driver's Test reads, so testing a bound system checks
// the same credential the manager will actually dial.
func (s *Server) applySSHBindingForTest(
	ctx context.Context,
	config map[string]any,
	secrets map[string]string,
) (map[string]any, error) {
	binding, bound, err := integration.ResolveSSHBinding(ctx, s.store, s.vault, config)
	if err != nil {
		return config, err
	}
	if !bound {
		return config, nil
	}
	for name := range integration.SSHSecretNames {
		secrets[name] = binding.Secret
	}
	return integration.ApplySSHBinding(config, binding), nil
}

func (s *Server) TestIntegrationConfig(ctx context.Context, req TestIntegrationConfigRequestObject) (TestIntegrationConfigResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResIntegrations, rbac.LevelConfigure) {
		return TestIntegrationConfig403JSONResponse{forbidden()}, nil
	}
	driver, ok := s.integrations.Driver(req.Body.Type)
	if !ok {
		return TestIntegrationConfig400JSONResponse{BadRequestJSONResponse(errBody("unknown_type", "unknown integration type "+req.Body.Type))}, nil
	}
	secrets := map[string]string{}
	if req.Body.Secrets != nil {
		secrets = *req.Body.Secrets
	}
	if !s.mayBindSSHHost(ctx, id, nil, req.Body.Config) {
		return TestIntegrationConfig403JSONResponse{forbidden()}, nil
	}
	config, berr := s.applySSHBindingForTest(ctx, req.Body.Config, secrets)
	if berr != nil {
		return TestIntegrationConfig200JSONResponse(TestConnectionResult{Ok: false, Message: berr.Error()}), nil
	}
	result := driver.Test(ctx, config, secrets)
	return TestIntegrationConfig200JSONResponse(toAPITestResult(result)), nil
}

func (s *Server) CreateIntegration(ctx context.Context, req CreateIntegrationRequestObject) (CreateIntegrationResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResIntegrations, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "integration.create", ResIntegrations)
		return CreateIntegration403JSONResponse{forbidden()}, nil
	}
	if _, ok := s.integrations.Driver(req.Body.Type); !ok {
		return CreateIntegration400JSONResponse{BadRequestJSONResponse(errBody("unknown_type", "unknown integration type "+req.Body.Type))}, nil
	}
	name := strings.TrimSpace(req.Body.Name)
	if name == "" {
		return CreateIntegration400JSONResponse{BadRequestJSONResponse(errBody("invalid_name", "a name is required"))}, nil
	}
	if !s.mayBindSSHHost(ctx, id, nil, req.Body.Config) {
		return CreateIntegration403JSONResponse{forbidden()}, nil
	}

	row, err := s.store.CreateIntegration(ctx, req.Body.Type, name, req.Body.Config)
	if err != nil {
		if store.IsUniqueViolation(err) {
			return CreateIntegration409JSONResponse{ConflictJSONResponse(errBody("name_taken", "an integration with that name already exists"))}, nil
		}
		return nil, err
	}
	if req.Body.Secrets != nil {
		for secretName, value := range *req.Body.Secrets {
			if err := s.vault.Seal(ctx, "integration:"+row.ID.String(), secretName, []byte(value)); err != nil {
				return nil, err
			}
		}
	}
	s.integrations.Apply(ctx, row.ID)
	s.audit.Log(ctx, actorFor(id), "integration.create", req.Body.Type+"/"+row.ID.String(), "success",
		map[string]any{"name": name, "type": req.Body.Type})

	fresh, err := s.store.GetIntegration(ctx, row.ID)
	if err != nil {
		return nil, err
	}
	return CreateIntegration201JSONResponse(toAPIIntegration(*fresh)), nil
}

func (s *Server) GetIntegration(ctx context.Context, req GetIntegrationRequestObject) (GetIntegrationResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	row, err := s.store.GetIntegration(ctx, req.Id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return GetIntegration404JSONResponse{notFoundErr("integration")}, nil
		}
		return nil, err
	}
	if !s.visibleUnder(ctx, id, row.Type+"/"+row.ID.String()) {
		return GetIntegration404JSONResponse{notFoundErr("integration")}, nil
	}
	return GetIntegration200JSONResponse(toAPIIntegration(*row)), nil
}

func (s *Server) UpdateIntegration(ctx context.Context, req UpdateIntegrationRequestObject) (UpdateIntegrationResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	row, err := s.store.GetIntegration(ctx, req.Id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return UpdateIntegration404JSONResponse{notFoundErr("integration")}, nil
		}
		return nil, err
	}
	resource := row.Type + "/" + row.ID.String()
	if !s.can(ctx, id, resource, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "integration.update", resource)
		return UpdateIntegration403JSONResponse{forbidden()}, nil
	}

	name := row.Name
	if req.Body.Name != nil {
		name = strings.TrimSpace(*req.Body.Name)
		if name == "" {
			return UpdateIntegration400JSONResponse{BadRequestJSONResponse(errBody("invalid_name", "a name is required"))}, nil
		}
	}
	config := any(row.Config)
	if req.Body.Config != nil {
		var prev map[string]any
		_ = json.Unmarshal(row.Config, &prev)
		if !s.mayBindSSHHost(ctx, id, prev, *req.Body.Config) {
			return UpdateIntegration403JSONResponse{forbidden()}, nil
		}
		if missing := s.secretsToReenter(ctx, row, prev, *req.Body.Config, req.Body.Secrets); len(missing) > 0 {
			return UpdateIntegration400JSONResponse{BadRequestJSONResponse(secretsRequired(missing))}, nil
		}
		config = *req.Body.Config
	}
	enabled := row.Enabled
	if req.Body.Enabled != nil {
		enabled = *req.Body.Enabled
	}
	if err := s.store.UpdateIntegration(ctx, row.ID, name, config, enabled); err != nil {
		if store.IsUniqueViolation(err) {
			return UpdateIntegration400JSONResponse{BadRequestJSONResponse(errBody("name_taken", "an integration with that name already exists"))}, nil
		}
		return nil, err
	}
	if req.Body.Secrets != nil {
		for secretName, value := range *req.Body.Secrets {
			if value == "" {
				continue // empty means "keep stored secret"
			}
			if err := s.vault.Seal(ctx, "integration:"+row.ID.String(), secretName, []byte(value)); err != nil {
				return nil, err
			}
		}
	}
	s.integrations.Apply(ctx, row.ID)
	s.audit.Log(ctx, actorFor(id), "integration.update", resource, "success",
		map[string]any{"name": name, "enabled": enabled})

	fresh, err := s.store.GetIntegration(ctx, row.ID)
	if err != nil {
		return nil, err
	}
	return UpdateIntegration200JSONResponse(toAPIIntegration(*fresh)), nil
}

func (s *Server) DeleteIntegration(ctx context.Context, req DeleteIntegrationRequestObject) (DeleteIntegrationResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	row, err := s.store.GetIntegration(ctx, req.Id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return DeleteIntegration404JSONResponse{notFoundErr("integration")}, nil
		}
		return nil, err
	}
	resource := row.Type + "/" + row.ID.String()
	if !s.can(ctx, id, resource, rbac.LevelDelete) {
		s.deniedAudit(ctx, id, "integration.delete", resource)
		return DeleteIntegration403JSONResponse{forbidden()}, nil
	}
	if err := s.store.DeleteIntegration(ctx, row.ID); err != nil {
		return nil, err
	}
	s.integrations.Remove(ctx, row.ID, row.Type)
	if err := s.store.DeleteSSHCredentialsUnder(ctx, resource); err != nil {
		s.logger.Error("delete ssh credentials for integration", "integration", row.ID, "error", err)
	}
	s.audit.Log(ctx, actorFor(id), "integration.delete", resource, "success", map[string]any{"name": row.Name})
	return DeleteIntegration204Response{}, nil
}

func (s *Server) TestIntegration(ctx context.Context, req TestIntegrationRequestObject) (TestIntegrationResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	row, err := s.store.GetIntegration(ctx, req.Id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return TestIntegration404JSONResponse{notFoundErr("integration")}, nil
		}
		return nil, err
	}
	resource := row.Type + "/" + row.ID.String()
	if !s.can(ctx, id, resource, rbac.LevelConfigure) {
		return TestIntegration403JSONResponse{forbidden()}, nil
	}
	driver, ok := s.integrations.Driver(row.Type)
	if !ok {
		return TestIntegration404JSONResponse{notFoundErr("integration type")}, nil
	}
	secrets := map[string]string{}
	for _, name := range driver.SecretNames() {
		raw, err := s.vault.Open(ctx, "integration:"+row.ID.String(), name)
		if err == nil {
			secrets[name] = string(raw)
		}
	}
	config, berr := s.applySSHBindingForTest(ctx, integration.ConfigFromJSON(row.Config), secrets)
	if berr != nil {
		return TestIntegration200JSONResponse(TestConnectionResult{Ok: false, Message: berr.Error()}), nil
	}
	result := driver.Test(ctx, config, secrets)
	return TestIntegration200JSONResponse(toAPITestResult(result)), nil
}

// ReloadIntegration drops a system's live connection and opens it again from
// the stored row — the operator's way to get a stuck system moving without
// editing it. Apply is the same path an edit takes, so the reconnect backoff
// resets too: a system two minutes into a retry wait is back within seconds
// if the upstream answers. Control-level, since nothing about the
// configuration changes.
func (s *Server) ReloadIntegration(ctx context.Context, req ReloadIntegrationRequestObject) (ReloadIntegrationResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	row, err := s.store.GetIntegration(ctx, req.Id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ReloadIntegration404JSONResponse{notFoundErr("integration")}, nil
		}
		return nil, err
	}
	resource := row.Type + "/" + row.ID.String()
	if !s.visibleUnder(ctx, id, resource) {
		return ReloadIntegration404JSONResponse{notFoundErr("integration")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelControl) {
		return ReloadIntegration403JSONResponse{forbidden()}, nil
	}
	if !row.Enabled {
		return ReloadIntegration409JSONResponse{ConflictJSONResponse(errBody("disabled", "this system is disabled — enable it to connect"))}, nil
	}
	// Apply stops the runner before it re-reads the row, and only relaunches
	// when that read succeeds. A request cancelled in between would leave the
	// system stopped until the next edit or restart, so the reload runs on a
	// context the client cannot take away.
	s.integrations.Apply(context.WithoutCancel(ctx), row.ID)
	s.audit.Log(ctx, actorFor(id), "integration.reload", resource, "success", map[string]any{"name": row.Name})
	return ReloadIntegration204Response{}, nil
}

func toAPIIntegration(row store.Integration) Integration {
	return Integration{
		Id:              row.ID,
		Type:            row.Type,
		Name:            row.Name,
		Config:          integration.ConfigFromJSON(row.Config),
		Enabled:         row.Enabled,
		Status:          IntegrationStatus(row.Status),
		StatusMessage:   row.StatusMessage,
		StatusChangedAt: row.StatusChangedAt,
		CreatedAt:       row.CreatedAt,
	}
}

func toAPITestResult(r integration.TestResult) TestConnectionResult {
	out := TestConnectionResult{Ok: r.OK, Message: r.Message}
	if r.Detail != nil {
		out.Detail = &r.Detail
	}
	return out
}

// mayBindSSHHost reports whether the caller may point an instance's config at
// the registry host it names. The driver then logs in with that host's stored
// key, so configure on the integration is not enough: it takes the same
// shell right as a terminal on the host. Keeping an existing binding is free —
// it was checked when it was made.
func (s *Server) mayBindSSHHost(ctx context.Context, id *httpctx.Identity, prev, next map[string]any) bool {
	raw, _ := next[integration.SSHHostIDKey].(string)
	if raw == "" {
		return true
	}
	if was, _ := prev[integration.SSHHostIDKey].(string); was == raw {
		return true
	}
	hostID, err := uuid.Parse(raw)
	if err != nil {
		return true // ResolveSSHBinding reports a malformed binding to the user
	}
	if !s.canShellOnSSHHost(ctx, id, hostID) {
		s.deniedAudit(ctx, id, "integration.ssh_binding", sshHostResource(hostID))
		return false
	}
	return true
}

// addressKey matches the config fields that decide where an instance's
// secrets are sent — its address, port, TLS posture or login. Drivers name
// these freely (host, baseUrl, sshHost, insecureTls, …), so it goes by name.
var addressKey = regexp.MustCompile(`(?i)host|url|address|endpoint|port|tls|fingerprint|socket|server|tailnet|user`)

// secretsToReenter lists the stored secrets an update would carry to a new
// destination. A token kept across an address change would be presented to
// whatever server the new address names, so every stored secret must come
// with the edit — or the edit is refused.
func (s *Server) secretsToReenter(ctx context.Context, row *store.Integration, prev, next map[string]any, given *map[string]string) []string {
	driver, ok := s.integrations.Driver(row.Type)
	if !ok {
		return nil
	}
	// Only the secrets the form asks for travel outward; the rest of the scope
	// (a Minecraft agent's inbound tokens, say) are never sent anywhere.
	return s.storedSecretsToReenter(ctx, "integration:"+row.ID.String(), driver.SecretNames(), prev, next, given)
}

// storedSecretsToReenter is the shared rule for every form with "leave empty
// to keep the stored secret": that only holds while the destination stays
// put. names limits it to the secrets that are sent outward; nil means every
// secret in the scope is.
func (s *Server) storedSecretsToReenter(ctx context.Context, scope string, names []string, prev, next map[string]any, given *map[string]string) []string {
	if !addressMoved(prev, next) {
		return nil
	}
	stored, err := s.store.SecretNames(ctx, scope)
	if err != nil {
		return []string{"secrets"} // unknown is refused, not waved through
	}
	var missing []string
	for _, name := range stored {
		if names != nil && !slices.Contains(names, name) {
			continue
		}
		if given == nil || (*given)[name] == "" {
			missing = append(missing, name)
		}
	}
	return missing
}

func addressMoved(prev, next map[string]any) bool {
	for k := range mergedKeys(prev, next) {
		if k == integration.SSHHostIDKey || !addressKey.MatchString(k) {
			continue
		}
		a, _ := json.Marshal(prev[k])
		b, _ := json.Marshal(next[k])
		if !bytes.Equal(a, b) {
			return true
		}
	}
	return false
}

// secretsRequired is the 400 body for an edit that moved a destination
// without re-entering its stored secrets.
func secretsRequired(missing []string) Error {
	return errBody("secrets_required",
		"the address changed, so the stored "+strings.Join(missing, ", ")+" must be entered again — a saved secret is never sent to a new destination")
}

func mergedKeys(a, b map[string]any) map[string]struct{} {
	out := make(map[string]struct{}, len(a)+len(b))
	for k := range a {
		out[k] = struct{}{}
	}
	for k := range b {
		out[k] = struct{}{}
	}
	return out
}
