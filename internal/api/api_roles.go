package api

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	"homehub/internal/httpctx"
	"homehub/internal/rbac"
	"homehub/internal/store"
)

func (s *Server) ListRoles(ctx context.Context, _ ListRolesRequestObject) (ListRolesResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResRoles, rbac.LevelView) {
		return ListRoles403JSONResponse{forbidden()}, nil
	}
	roles, err := s.store.ListRoles(ctx)
	if err != nil {
		return nil, err
	}
	counts, err := s.store.RoleMemberCounts(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Role, 0, len(roles))
	for _, r := range roles {
		grants, err := s.store.RoleGrants(ctx, r.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, toAPIRole(r, grants, counts[r.ID]))
	}
	return ListRoles200JSONResponse(out), nil
}

func (s *Server) CreateRole(ctx context.Context, req CreateRoleRequestObject) (CreateRoleResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResRoles, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "role.create", ResRoles)
		return CreateRole403JSONResponse{forbidden()}, nil
	}
	name := strings.TrimSpace(req.Body.Name)
	if name == "" {
		return CreateRole400JSONResponse{BadRequestJSONResponse(errBody("invalid_name", "role name is required"))}, nil
	}
	var specs []store.GrantSpec
	if req.Body.Grants != nil {
		var msg string
		specs, msg = grantSpecsFromInput(*req.Body.Grants)
		if msg != "" {
			return CreateRole400JSONResponse{BadRequestJSONResponse(errBody("invalid_grant", msg))}, nil
		}
	}
	if ok, err := s.specsWithinCaller(ctx, id, specs); err != nil {
		return nil, err
	} else if !ok {
		s.deniedAudit(ctx, id, "role.create", ResRoles)
		return CreateRole403JSONResponse{errExceedsOwn()}, nil
	}
	role, err := s.store.CreateRole(ctx, name, deref(req.Body.Description), false, specs)
	if err != nil {
		if store.IsUniqueViolation(err) {
			return CreateRole409JSONResponse{ConflictJSONResponse(errBody("role_exists", "a role with that name already exists"))}, nil
		}
		return nil, err
	}
	if req.Body.HideSignals != nil && *req.Body.HideSignals {
		if err := s.store.SetRoleHideSignals(ctx, role.ID, true); err != nil {
			return nil, err
		}
		role.HideSignals = true
	}
	s.audit.Log(ctx, actorFor(id), "role.create", ResRoles, "success",
		map[string]any{"role": role.Name, "grants": len(specs)})

	grants, err := s.store.RoleGrants(ctx, role.ID)
	if err != nil {
		return nil, err
	}
	return CreateRole201JSONResponse(toAPIRole(*role, grants, 0)), nil
}

func (s *Server) GetRole(ctx context.Context, req GetRoleRequestObject) (GetRoleResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResRoles, rbac.LevelView) {
		return GetRole403JSONResponse{forbidden()}, nil
	}
	role, grants, count, err := s.loadRole(ctx, req.Id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return GetRole404JSONResponse{notFoundErr("role")}, nil
		}
		return nil, err
	}
	return GetRole200JSONResponse(toAPIRole(*role, grants, count)), nil
}

func (s *Server) UpdateRole(ctx context.Context, req UpdateRoleRequestObject) (UpdateRoleResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResRoles, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "role.update", ResRoles)
		return UpdateRole403JSONResponse{forbidden()}, nil
	}
	role, existingGrants, _, err := s.loadRole(ctx, req.Id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return UpdateRole404JSONResponse{notFoundErr("role")}, nil
		}
		return nil, err
	}
	// hideSignals is a display preference, not a grant/identity change, so it is
	// editable on ANY role, built-in ones included (hiding signals from e.g. the
	// Viewer role is a common need). Everything else stays locked on system roles.
	if req.Body.HideSignals != nil {
		if err := s.store.SetRoleHideSignals(ctx, role.ID, *req.Body.HideSignals); err != nil {
			return nil, err
		}
		s.audit.Log(ctx, actorFor(id), "role.update", ResRoles, "success",
			map[string]any{"role": role.Name, "hideSignals": *req.Body.HideSignals})
	}
	if role.IsSystem {
		if req.Body.Name != nil || req.Body.Description != nil || req.Body.Grants != nil {
			return UpdateRole409JSONResponse{ConflictJSONResponse(errBody("system_role", "built-in roles cannot be edited (except hiding signals) — create a custom role instead"))}, nil
		}
		fresh, grants, count, err := s.loadRole(ctx, role.ID)
		if err != nil {
			return nil, err
		}
		return UpdateRole200JSONResponse(toAPIRole(*fresh, grants, count)), nil
	}

	name := role.Name
	if req.Body.Name != nil {
		name = strings.TrimSpace(*req.Body.Name)
		if name == "" {
			return UpdateRole400JSONResponse{BadRequestJSONResponse(errBody("invalid_name", "role name is required"))}, nil
		}
	}
	description := role.Description
	if req.Body.Description != nil {
		description = *req.Body.Description
	}

	specs := grantsToSpecs(existingGrants)
	if req.Body.Grants != nil {
		var msg string
		specs, msg = grantSpecsFromInput(*req.Body.Grants)
		if msg != "" {
			return UpdateRole400JSONResponse{BadRequestJSONResponse(errBody("invalid_grant", msg))}, nil
		}
	}
	// Both sides: a role above the caller is not theirs to rewrite, and the
	// rewrite may not reach above them either.
	for _, set := range [][]store.GrantSpec{grantsToSpecs(existingGrants), specs} {
		if ok, err := s.specsWithinCaller(ctx, id, set); err != nil {
			return nil, err
		} else if !ok {
			s.deniedAudit(ctx, id, "role.update", ResRoles)
			return UpdateRole403JSONResponse{errExceedsOwn()}, nil
		}
	}

	if err := s.store.UpdateRole(ctx, role.ID, name, description, specs); err != nil {
		if store.IsUniqueViolation(err) {
			return UpdateRole409JSONResponse{ConflictJSONResponse(errBody("role_exists", "a role with that name already exists"))}, nil
		}
		return nil, err
	}
	// Role edits affect every member — flush all cached grants.
	s.rbac.InvalidateAll()
	s.audit.Log(ctx, actorFor(id), "role.update", ResRoles, "success",
		map[string]any{"role": name, "grants": len(specs)})

	fresh, grants, count, err := s.loadRole(ctx, role.ID)
	if err != nil {
		return nil, err
	}
	return UpdateRole200JSONResponse(toAPIRole(*fresh, grants, count)), nil
}

func (s *Server) DeleteRole(ctx context.Context, req DeleteRoleRequestObject) (DeleteRoleResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResRoles, rbac.LevelDelete) {
		s.deniedAudit(ctx, id, "role.delete", ResRoles)
		return DeleteRole403JSONResponse{forbidden()}, nil
	}
	role, err := s.store.GetRole(ctx, req.Id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return DeleteRole404JSONResponse{notFoundErr("role")}, nil
		}
		return nil, err
	}
	if role.IsSystem {
		return DeleteRole409JSONResponse{ConflictJSONResponse(errBody("system_role", "built-in roles cannot be deleted"))}, nil
	}
	// Held to the same bar as rewriting it: a role above the caller is not
	// theirs to remove.
	if ok, err := s.rolesWithinCaller(ctx, id, []uuid.UUID{role.ID}); err != nil {
		return nil, err
	} else if !ok {
		s.deniedAudit(ctx, id, "role.delete", ResRoles)
		return DeleteRole403JSONResponse{errExceedsOwn()}, nil
	}
	if err := s.store.DeleteRole(ctx, role.ID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return DeleteRole404JSONResponse{notFoundErr("role")}, nil
		}
		return nil, err
	}
	s.rbac.InvalidateAll()
	s.audit.Log(ctx, actorFor(id), "role.delete", ResRoles, "success", map[string]any{"role": role.Name})
	return DeleteRole204Response{}, nil
}

func (s *Server) loadRole(ctx context.Context, id uuid.UUID) (*store.Role, []store.Grant, int, error) {
	role, err := s.store.GetRole(ctx, id)
	if err != nil {
		return nil, nil, 0, err
	}
	grants, err := s.store.RoleGrants(ctx, id)
	if err != nil {
		return nil, nil, 0, err
	}
	counts, err := s.store.RoleMemberCounts(ctx)
	if err != nil {
		return nil, nil, 0, err
	}
	return role, grants, counts[id], nil
}

func toAPIRole(r store.Role, grants []store.Grant, memberCount int) Role {
	return Role{
		Id:          r.ID,
		Name:        r.Name,
		Description: r.Description,
		IsSystem:    r.IsSystem,
		HideSignals: r.HideSignals,
		MemberCount: memberCount,
		Grants:      toAPIGrants(grants),
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}

func grantsToSpecs(grants []store.Grant) []store.GrantSpec {
	specs := make([]store.GrantSpec, 0, len(grants))
	for _, g := range grants {
		specs = append(specs, store.GrantSpec{Selector: g.Selector, Levels: g.Levels, Effect: g.Effect})
	}
	return specs
}
