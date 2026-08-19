package api

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/TheOriUHD/homehub/internal/httpctx"
	"github.com/TheOriUHD/homehub/internal/integrations/tailscale"
	"github.com/TheOriUHD/homehub/internal/rbac"
	"github.com/TheOriUHD/homehub/internal/store"
)

// tsConn resolves an integration id to a live Tailscale connection,
// enforcing tailnet visibility.
func (s *Server) tsConn(ctx context.Context, id *httpctx.Identity, instID uuid.UUID) (*tailscale.Conn, string, error) {
	row, err := s.store.GetIntegration(ctx, instID)
	if err != nil || row.Type != tailscale.TypeID {
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
	tc, ok := conn.(*tailscale.Conn)
	if !ok {
		return nil, resource, store.ErrNotFound
	}
	return tc, resource, nil
}

// tsDeviceAccess authorizes one device at a level.
func (s *Server) tsDeviceAccess(ctx context.Context, id *httpctx.Identity, instID uuid.UUID, deviceID string, level rbac.Level) (*tailscale.Conn, string, bool, error) {
	conn, resource, err := s.tsConn(ctx, id, instID)
	if err != nil {
		return nil, resource, false, err
	}
	deviceResource := resource + "/device/" + deviceID
	if !s.can(ctx, id, deviceResource, level) {
		return conn, deviceResource, false, nil
	}
	return conn, deviceResource, true, nil
}

func tsTimeStr(t tailscale.TsTime) *string {
	if t.IsZero() {
		return nil
	}
	v := t.UTC().Format(time.RFC3339)
	return &v
}

func toAPITsDevice(d tailscale.Device) TsDevice {
	name := d.Name
	if i := strings.IndexByte(name, '.'); i > 0 {
		name = name[:i]
	}
	out := TsDevice{
		Id:                d.ID,
		Name:              name,
		Hostname:          d.Hostname,
		User:              d.User,
		Addresses:         d.Addresses,
		Os:                d.OS,
		ClientVersion:     d.ClientVersion,
		UpdateAvailable:   d.UpdateAvailable,
		Authorized:        d.Authorized,
		KeyExpiryDisabled: d.KeyExpiryDisabled,
		Online:            d.Online(),
		External:          d.IsExternal,
		Tags:              d.Tags,
		AdvertisedRoutes:  d.AdvertisedRoutes,
		EnabledRoutes:     d.EnabledRoutes,
	}
	if out.Addresses == nil {
		out.Addresses = []string{}
	}
	if out.Tags == nil {
		out.Tags = []string{}
	}
	if out.AdvertisedRoutes == nil {
		out.AdvertisedRoutes = []string{}
	}
	if out.EnabledRoutes == nil {
		out.EnabledRoutes = []string{}
	}
	out.Created = tsTimeStr(d.Created)
	out.Expires = tsTimeStr(d.Expires)
	out.LastSeen = tsTimeStr(d.LastSeen)
	return out
}

func toAPITsKey(k tailscale.Key) TsKey {
	out := TsKey{
		Id:            k.ID,
		Description:   k.Description,
		Invalid:       k.Invalid,
		Reusable:      k.Capabilities.Devices.Create.Reusable,
		Ephemeral:     k.Capabilities.Devices.Create.Ephemeral,
		Preauthorized: k.Capabilities.Devices.Create.Preauthorized,
		Tags:          k.Capabilities.Devices.Create.Tags,
	}
	if out.Tags == nil {
		out.Tags = []string{}
	}
	out.Created = tsTimeStr(k.Created)
	out.Expires = tsTimeStr(k.Expires)
	out.Revoked = tsTimeStr(k.Revoked)
	return out
}

func (s *Server) TsListInstances(ctx context.Context, _ TsListInstancesRequestObject) (TsListInstancesResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	rows, err := s.store.ListIntegrations(ctx)
	if err != nil {
		return nil, err
	}
	out := []TsTailnet{}
	for _, row := range rows {
		if row.Type != tailscale.TypeID || !row.Enabled {
			continue
		}
		resource := row.Type + "/" + row.ID.String()
		if !s.visibleUnder(ctx, id, resource) {
			continue
		}
		tailnet := TsTailnet{
			Id: row.ID, Name: row.Name,
			Status: TsTailnetStatus(row.Status), StatusMessage: row.StatusMessage,
		}
		if conn, cerr := s.integrations.Connection(row.ID); cerr == nil {
			if tc, ok := conn.(*tailscale.Conn); ok {
				ictx, cancel := context.WithTimeout(ctx, 10*time.Second)
				if devices, derr := tc.Devices(ictx); derr == nil {
					for _, d := range s.visibleTsDevices(ctx, id, resource, devices) {
						tailnet.DeviceCount++
						if d.Online() {
							tailnet.OnlineCount++
						}
					}
				}
				cancel()
			}
		}
		out = append(out, tailnet)
	}
	return TsListInstances200JSONResponse(out), nil
}

// visibleTsDevices filters devices by per-device view grants.
func (s *Server) visibleTsDevices(ctx context.Context, id *httpctx.Identity, resource string, devices []tailscale.Device) []tailscale.Device {
	out := make([]tailscale.Device, 0, len(devices))
	for _, d := range devices {
		if s.can(ctx, id, resource+"/device/"+d.ID, rbac.LevelView) {
			out = append(out, d)
		}
	}
	return out
}

func (s *Server) TsDevices(ctx context.Context, req TsDevicesRequestObject) (TsDevicesResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, resource, err := s.tsConn(ctx, id, req.Id)
	if err == store.ErrNotFound {
		return TsDevices404JSONResponse{notFoundErr("tailnet")}, nil
	}
	if err != nil {
		return TsDevices502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	devices, err := conn.Devices(ctx)
	if err != nil {
		return TsDevices502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	out := []TsDevice{}
	for _, d := range s.visibleTsDevices(ctx, id, resource, devices) {
		out = append(out, toAPITsDevice(d))
	}
	return TsDevices200JSONResponse(out), nil
}

// tsDeviceAction wraps the shared authorize-act-audit flow for device verbs.
func (s *Server) tsDeviceAction(ctx context.Context, instID uuid.UUID, deviceID, auditAction string,
	act func(conn *tailscale.Conn, ctx context.Context) error,
) (int, string) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return 401, "unauthenticated"
	}
	conn, resource, allowed, err := s.tsDeviceAccess(ctx, id, instID, deviceID, rbac.LevelControl)
	if err == store.ErrNotFound {
		return 404, ""
	}
	if err != nil {
		return 502, err.Error()
	}
	if !allowed {
		s.deniedAudit(ctx, id, auditAction, resource)
		return 403, ""
	}
	actx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := act(conn, actx); err != nil {
		s.audit.Log(ctx, actorFor(id), auditAction, resource, "error", map[string]any{"error": err.Error()})
		return 502, err.Error()
	}
	s.audit.Log(ctx, actorFor(id), auditAction, resource, "success", nil)
	return 204, ""
}

func (s *Server) TsSetAuthorized(ctx context.Context, req TsSetAuthorizedRequestObject) (TsSetAuthorizedResponseObject, error) {
	code, msg := s.tsDeviceAction(ctx, req.Id, req.DeviceId, "tailscale.device_authorized",
		func(conn *tailscale.Conn, ctx context.Context) error {
			return conn.SetAuthorized(ctx, req.DeviceId, req.Body.Authorized)
		})
	switch code {
	case 204:
		return TsSetAuthorized204Response{}, nil
	case 403:
		return TsSetAuthorized403JSONResponse{forbidden()}, nil
	case 404:
		return TsSetAuthorized404JSONResponse{notFoundErr("device")}, nil
	case 401:
		return nil, ErrUnauthenticated
	default:
		return TsSetAuthorized502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", msg))}, nil
	}
}

func (s *Server) TsSetRoutes(ctx context.Context, req TsSetRoutesRequestObject) (TsSetRoutesResponseObject, error) {
	code, msg := s.tsDeviceAction(ctx, req.Id, req.DeviceId, "tailscale.device_routes",
		func(conn *tailscale.Conn, ctx context.Context) error {
			return conn.SetRoutes(ctx, req.DeviceId, req.Body.Routes)
		})
	switch code {
	case 204:
		return TsSetRoutes204Response{}, nil
	case 403:
		return TsSetRoutes403JSONResponse{forbidden()}, nil
	case 404:
		return TsSetRoutes404JSONResponse{notFoundErr("device")}, nil
	case 401:
		return nil, ErrUnauthenticated
	default:
		return TsSetRoutes502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", msg))}, nil
	}
}

func (s *Server) TsSetKeyExpiry(ctx context.Context, req TsSetKeyExpiryRequestObject) (TsSetKeyExpiryResponseObject, error) {
	code, msg := s.tsDeviceAction(ctx, req.Id, req.DeviceId, "tailscale.device_key_expiry",
		func(conn *tailscale.Conn, ctx context.Context) error {
			return conn.SetKeyExpiryDisabled(ctx, req.DeviceId, req.Body.KeyExpiryDisabled)
		})
	switch code {
	case 204:
		return TsSetKeyExpiry204Response{}, nil
	case 403:
		return TsSetKeyExpiry403JSONResponse{forbidden()}, nil
	case 404:
		return TsSetKeyExpiry404JSONResponse{notFoundErr("device")}, nil
	case 401:
		return nil, ErrUnauthenticated
	default:
		return TsSetKeyExpiry502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", msg))}, nil
	}
}

func (s *Server) TsExpireKey(ctx context.Context, req TsExpireKeyRequestObject) (TsExpireKeyResponseObject, error) {
	code, msg := s.tsDeviceAction(ctx, req.Id, req.DeviceId, "tailscale.device_expire",
		func(conn *tailscale.Conn, ctx context.Context) error {
			return conn.ExpireKey(ctx, req.DeviceId)
		})
	switch code {
	case 204:
		return TsExpireKey204Response{}, nil
	case 403:
		return TsExpireKey403JSONResponse{forbidden()}, nil
	case 404:
		return TsExpireKey404JSONResponse{notFoundErr("device")}, nil
	case 401:
		return nil, ErrUnauthenticated
	default:
		return TsExpireKey502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", msg))}, nil
	}
}

func (s *Server) TsDeleteDevice(ctx context.Context, req TsDeleteDeviceRequestObject) (TsDeleteDeviceResponseObject, error) {
	code, msg := s.tsDeviceAction(ctx, req.Id, req.DeviceId, "tailscale.device_delete",
		func(conn *tailscale.Conn, ctx context.Context) error {
			return conn.DeleteDevice(ctx, req.DeviceId)
		})
	switch code {
	case 204:
		return TsDeleteDevice204Response{}, nil
	case 403:
		return TsDeleteDevice403JSONResponse{forbidden()}, nil
	case 404:
		return TsDeleteDevice404JSONResponse{notFoundErr("device")}, nil
	case 401:
		return nil, ErrUnauthenticated
	default:
		return TsDeleteDevice502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", msg))}, nil
	}
}

func (s *Server) TsKeys(ctx context.Context, req TsKeysRequestObject) (TsKeysResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, resource, err := s.tsConn(ctx, id, req.Id)
	if err == store.ErrNotFound {
		return TsKeys404JSONResponse{notFoundErr("tailnet")}, nil
	}
	if err != nil {
		return TsKeys502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	// Pre-auth key metadata (tags, reusability, expiry) describes how devices
	// join the tailnet — network-level recon. Mere device-subtree visibility
	// (which tsConn checks) is not enough: require view on the tailnet root, so
	// a member granted only one device can't enumerate join keys. Tailnet-level
	// viewers still see the list; create/revoke remain configure-level.
	if !s.can(ctx, id, resource, rbac.LevelView) {
		return TsKeys200JSONResponse([]TsKey{}), nil
	}
	keys, err := conn.Keys(ctx)
	if err != nil {
		return TsKeys502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	out := make([]TsKey, 0, len(keys))
	for _, k := range keys {
		out = append(out, toAPITsKey(k))
	}
	return TsKeys200JSONResponse(out), nil
}

func (s *Server) TsCreateKey(ctx context.Context, req TsCreateKeyRequestObject) (TsCreateKeyResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, resource, err := s.tsConn(ctx, id, req.Id)
	if err == store.ErrNotFound {
		return TsCreateKey404JSONResponse{notFoundErr("tailnet")}, nil
	}
	if err != nil {
		return TsCreateKey502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	// Minting a key grants network access — configure-level, like secrets.
	if !s.can(ctx, id, resource, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "tailscale.key_create", resource)
		return TsCreateKey403JSONResponse{forbidden()}, nil
	}
	spec := tailscale.CreateKeySpec{}
	if req.Body.Description != nil {
		spec.Description = *req.Body.Description
	}
	if req.Body.ExpiryDays != nil {
		spec.ExpirySeconds = int64(*req.Body.ExpiryDays) * 86400
	}
	if req.Body.Reusable != nil {
		spec.Reusable = *req.Body.Reusable
	}
	if req.Body.Ephemeral != nil {
		spec.Ephemeral = *req.Body.Ephemeral
	}
	if req.Body.Preauthorized != nil {
		spec.Preauthorized = *req.Body.Preauthorized
	}
	if req.Body.Tags != nil {
		spec.Tags = *req.Body.Tags
	}
	key, err := conn.CreateKey(ctx, spec)
	if err != nil {
		s.audit.Log(ctx, actorFor(id), "tailscale.key_create", resource, "error", map[string]any{"error": err.Error()})
		return TsCreateKey502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	s.audit.Log(ctx, actorFor(id), "tailscale.key_create", resource, "success",
		map[string]any{"keyId": key.ID, "description": key.Description, "reusable": spec.Reusable})
	out := TsCreatedKey{Key: key.Key}
	base := toAPITsKey(*key)
	out.Id = base.Id
	out.Description = base.Description
	out.Created = base.Created
	out.Expires = base.Expires
	out.Revoked = base.Revoked
	out.Invalid = base.Invalid
	out.Reusable = base.Reusable
	out.Ephemeral = base.Ephemeral
	out.Preauthorized = base.Preauthorized
	out.Tags = base.Tags
	return TsCreateKey200JSONResponse(out), nil
}

func (s *Server) TsRevokeKey(ctx context.Context, req TsRevokeKeyRequestObject) (TsRevokeKeyResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, resource, err := s.tsConn(ctx, id, req.Id)
	if err == store.ErrNotFound {
		return TsRevokeKey404JSONResponse{notFoundErr("tailnet")}, nil
	}
	if err != nil {
		return TsRevokeKey502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "tailscale.key_revoke", resource)
		return TsRevokeKey403JSONResponse{forbidden()}, nil
	}
	if err := conn.RevokeKey(ctx, req.KeyId); err != nil {
		s.audit.Log(ctx, actorFor(id), "tailscale.key_revoke", resource, "error", map[string]any{"keyId": req.KeyId, "error": err.Error()})
		return TsRevokeKey502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	s.audit.Log(ctx, actorFor(id), "tailscale.key_revoke", resource, "success", map[string]any{"keyId": req.KeyId})
	return TsRevokeKey204Response{}, nil
}

func (s *Server) TsDns(ctx context.Context, req TsDnsRequestObject) (TsDnsResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, _, err := s.tsConn(ctx, id, req.Id)
	if err == store.ErrNotFound {
		return TsDns404JSONResponse{notFoundErr("tailnet")}, nil
	}
	if err != nil {
		return TsDns502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	dns, err := conn.DNS(ctx)
	if err != nil {
		return TsDns502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	out := TsDns{Nameservers: dns.Nameservers, MagicDns: dns.MagicDNS, SearchPaths: dns.SearchPaths}
	if out.Nameservers == nil {
		out.Nameservers = []string{}
	}
	if out.SearchPaths == nil {
		out.SearchPaths = []string{}
	}
	return TsDns200JSONResponse(out), nil
}

func (s *Server) TsSetDns(ctx context.Context, req TsSetDnsRequestObject) (TsSetDnsResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, resource, err := s.tsConn(ctx, id, req.Id)
	if err == store.ErrNotFound {
		return TsSetDns404JSONResponse{notFoundErr("tailnet")}, nil
	}
	if err != nil {
		return TsSetDns502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "tailscale.dns_update", resource)
		return TsSetDns403JSONResponse{forbidden()}, nil
	}
	settings := tailscale.DNSSettings{
		Nameservers: req.Body.Nameservers,
		MagicDNS:    req.Body.MagicDns,
		SearchPaths: req.Body.SearchPaths,
	}
	if err := conn.SetDNS(ctx, settings); err != nil {
		s.audit.Log(ctx, actorFor(id), "tailscale.dns_update", resource, "error", map[string]any{"error": err.Error()})
		return TsSetDns502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	s.audit.Log(ctx, actorFor(id), "tailscale.dns_update", resource, "success",
		map[string]any{"nameservers": settings.Nameservers, "magicDns": settings.MagicDNS})
	return TsSetDns204Response{}, nil
}
