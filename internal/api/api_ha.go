package api

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"homehub/internal/httpctx"
	"homehub/internal/integrations/homeassistant"
	"homehub/internal/rbac"
	"homehub/internal/store"
)

// haConn resolves an integration id to a live Home Assistant connection,
// enforcing instance visibility. Invisible and non-HA ids are both a 404 so
// unshared instances do not leak existence.
func (s *Server) haConn(ctx context.Context, id *httpctx.Identity, instID uuid.UUID) (*homeassistant.Conn, *store.Integration, string, error) {
	row, err := s.store.GetIntegration(ctx, instID)
	if err != nil || row.Type != homeassistant.TypeID {
		return nil, nil, "", store.ErrNotFound
	}
	resource := row.Type + "/" + instID.String()
	if !s.visibleUnder(ctx, id, resource) {
		return nil, nil, "", store.ErrNotFound
	}
	conn, err := s.integrations.Connection(instID)
	if err != nil {
		return nil, row, resource, err
	}
	hac, ok := conn.(*homeassistant.Conn)
	if !ok {
		return nil, row, resource, store.ErrNotFound
	}
	return hac, row, resource, nil
}

func toAPIHaEntity(e homeassistant.Entity) HaEntity {
	out := HaEntity{
		EntityId:    e.EntityID,
		Domain:      e.Domain,
		ObjectId:    e.ObjectID,
		Name:        e.Name,
		State:       e.State,
		Attributes:  e.Attributes,
		LastChanged: e.LastChanged,
		LastUpdated: e.LastUpdated,
	}
	if e.AreaID != "" {
		out.AreaId = &e.AreaID
	}
	if e.Category != "" {
		out.Category = &e.Category
	}
	return out
}

// visibleHaEntities filters an instance's entities to those the caller may
// view; the per-entity checks ride the engine's per-principal grant cache.
func (s *Server) visibleHaEntities(ctx context.Context, id *httpctx.Identity, resource string, conn *homeassistant.Conn) []homeassistant.Entity {
	all := conn.Entities()
	out := make([]homeassistant.Entity, 0, len(all))
	for _, e := range all {
		if s.can(ctx, id, resource+"/"+e.Domain+"/"+e.ObjectID, rbac.LevelView) {
			out = append(out, e)
		}
	}
	return out
}

func (s *Server) HaListInstances(ctx context.Context, _ HaListInstancesRequestObject) (HaListInstancesResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	rows, err := s.store.ListIntegrations(ctx)
	if err != nil {
		return nil, err
	}
	out := []HaInstance{}
	for _, row := range rows {
		if row.Type != homeassistant.TypeID || !row.Enabled {
			continue
		}
		resource := row.Type + "/" + row.ID.String()
		if !s.visibleUnder(ctx, id, resource) {
			continue
		}
		instance := HaInstance{
			Id:            row.ID,
			Name:          row.Name,
			Status:        HaInstanceStatus(row.Status),
			StatusMessage: row.StatusMessage,
		}
		if conn, cerr := s.integrations.Connection(row.ID); cerr == nil {
			if hac, ok := conn.(*homeassistant.Conn); ok {
				info := hac.Info()
				instance.Info = &HaInfo{
					Version:      info.Version,
					LocationName: info.LocationName,
				}
				if info.TimeZone != "" {
					instance.Info.TimeZone = &info.TimeZone
				}
				if info.Country != "" {
					instance.Info.Country = &info.Country
				}
				instance.EntityCount = len(s.visibleHaEntities(ctx, id, resource, hac))
			}
		}
		out = append(out, instance)
	}
	return HaListInstances200JSONResponse(out), nil
}

func (s *Server) HaListEntities(ctx context.Context, req HaListEntitiesRequestObject) (HaListEntitiesResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, _, resource, err := s.haConn(ctx, id, req.Id)
	if err != nil {
		if err == store.ErrNotFound {
			return HaListEntities404JSONResponse{notFoundErr("instance")}, nil
		}
		return HaListEntities502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	visible := s.visibleHaEntities(ctx, id, resource, conn)

	entities := make([]HaEntity, 0, len(visible))
	usedAreas := map[string]bool{}
	for _, e := range visible {
		if req.Params.Domain != nil && e.Domain != *req.Params.Domain {
			continue
		}
		entities = append(entities, toAPIHaEntity(e))
		if e.AreaID != "" {
			usedAreas[e.AreaID] = true
		}
	}
	areas := []HaArea{}
	for _, a := range conn.Areas() {
		if usedAreas[a.ID] {
			areas = append(areas, HaArea{Id: a.ID, Name: a.Name})
		}
	}
	return HaListEntities200JSONResponse{Entities: entities, Areas: areas}, nil
}

func (s *Server) HaGetEntity(ctx context.Context, req HaGetEntityRequestObject) (HaGetEntityResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, _, resource, err := s.haConn(ctx, id, req.Id)
	if err != nil {
		if err == store.ErrNotFound {
			return HaGetEntity404JSONResponse{notFoundErr("instance")}, nil
		}
		return HaGetEntity502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	entity, ok := conn.Entity(req.EntityId)
	if !ok {
		return HaGetEntity404JSONResponse{notFoundErr("entity")}, nil
	}
	if !s.can(ctx, id, resource+"/"+entity.Domain+"/"+entity.ObjectID, rbac.LevelView) {
		return HaGetEntity404JSONResponse{notFoundErr("entity")}, nil
	}
	return HaGetEntity200JSONResponse(toAPIHaEntity(entity)), nil
}

func (s *Server) HaGetDashboard(ctx context.Context, req HaGetDashboardRequestObject) (HaGetDashboardResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if _, _, _, err := s.haConn(ctx, id, req.Id); err != nil && err == store.ErrNotFound {
		return HaGetDashboard404JSONResponse{notFoundErr("instance")}, nil
	}
	raw, err := s.store.GetHaDashboard(ctx, id.User.ID, req.Id)
	if err != nil {
		return nil, err
	}
	var board HaDashboard
	if err := json.Unmarshal(raw, &board); err != nil || board.Cards == nil {
		board = HaDashboard{Cards: []HaDashboardCard{}}
	}
	return HaGetDashboard200JSONResponse(board), nil
}

func (s *Server) HaPutDashboard(ctx context.Context, req HaPutDashboardRequestObject) (HaPutDashboardResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if _, _, _, err := s.haConn(ctx, id, req.Id); err != nil && err == store.ErrNotFound {
		return HaPutDashboard404JSONResponse{notFoundErr("instance")}, nil
	}
	board := *req.Body
	if len(board.Cards) > 100 {
		return HaPutDashboard400JSONResponse{BadRequestJSONResponse(errBody("too_many_cards", "a dashboard holds at most 100 cards"))}, nil
	}
	for _, card := range board.Cards {
		if card.Type != Entity && card.Type != Group {
			return HaPutDashboard400JSONResponse{BadRequestJSONResponse(errBody("bad_card", "card type must be entity or group"))}, nil
		}
		if len(card.EntityIds) == 0 || len(card.EntityIds) > 50 {
			return HaPutDashboard400JSONResponse{BadRequestJSONResponse(errBody("bad_card", "each card needs 1–50 entities"))}, nil
		}
	}
	raw, err := json.Marshal(board)
	if err != nil {
		return nil, err
	}
	if err := s.store.PutHaDashboard(ctx, id.User.ID, req.Id, raw); err != nil {
		return nil, err
	}
	return HaPutDashboard204Response{}, nil
}

// validHaServiceName mirrors the spec pattern ^[a-z0-9_]+$.
func validHaServiceName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return true
}

func (s *Server) HaCallService(ctx context.Context, req HaCallServiceRequestObject) (HaCallServiceResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, _, resource, err := s.haConn(ctx, id, req.Id)
	if err != nil {
		if err == store.ErrNotFound {
			return HaCallService404JSONResponse{notFoundErr("instance")}, nil
		}
		return HaCallService502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	entity, ok := conn.Entity(req.EntityId)
	if !ok {
		return HaCallService404JSONResponse{notFoundErr("entity")}, nil
	}
	entityRes := resource + "/" + entity.Domain + "/" + entity.ObjectID
	if !s.can(ctx, id, entityRes, rbac.LevelView) {
		return HaCallService404JSONResponse{notFoundErr("entity")}, nil
	}
	if !s.can(ctx, id, entityRes, rbac.LevelControl) {
		s.deniedAudit(ctx, id, "ha.service_call", entityRes)
		return HaCallService403JSONResponse{forbidden()}, nil
	}
	if !validHaServiceName(req.Body.Service) {
		return HaCallService400JSONResponse{BadRequestJSONResponse(errBody("invalid_service", "service names are lowercase snake_case, e.g. turn_on"))}, nil
	}
	// The service always runs in the entity's own domain: a grant on
	// homeassistant/<id>/light/* can never reach another domain's services.
	domain := entity.Domain
	var data map[string]any
	if req.Body.Data != nil {
		data = *req.Body.Data
	}
	if err := conn.CallService(ctx, domain, req.Body.Service, entity.EntityID, data); err != nil {
		s.audit.Log(ctx, actorFor(id), "ha.service_call", entityRes, "error",
			map[string]any{"service": req.Body.Service, "error": err.Error()})
		return HaCallService502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	detail := map[string]any{"service": req.Body.Service}
	if len(data) > 0 {
		detail["data"] = data
	}
	s.audit.Log(ctx, actorFor(id), "ha.service_call", entityRes, "success", detail)
	return HaCallService204Response{}, nil
}
