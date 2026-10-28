package api

import (
	"context"

	"github.com/google/uuid"

	"homehub/internal/httpctx"
	"homehub/internal/rbac"
	"homehub/internal/store"
)

// No one can hand out more than they hold. Managing users or roles is a
// configure grant on system/users or system/roles, and without this rule that
// grant was Owner in disguise: assign yourself the Owner role, write a role
// with "*", or reset an Owner's password and log in as them. Each check below
// compares against the caller's own grants, so an Owner is unaffected and a
// delegated user admin can manage exactly the people below them.

// errExceedsOwn is the 403 body for an attempt to grant or touch more than
// the caller holds.
func errExceedsOwn() ForbiddenJSONResponse {
	return ForbiddenJSONResponse(errBody("exceeds_own_rights",
		"you can only grant rights you hold yourself, and only manage users whose rights you hold"))
}

func (s *Server) callerGrants(ctx context.Context, id *httpctx.Identity) ([]store.Grant, error) {
	return s.store.UserGrants(ctx, id.User.ID)
}

// specsWithinCaller reports whether the caller holds every allow grant listed.
func (s *Server) specsWithinCaller(ctx context.Context, id *httpctx.Identity, specs []store.GrantSpec) (bool, error) {
	mine, err := s.callerGrants(ctx, id)
	if err != nil {
		return false, err
	}
	return rbac.CoversAll(mine, specs), nil
}

// rolesWithinCaller reports whether the caller holds everything the given
// roles grant.
func (s *Server) rolesWithinCaller(ctx context.Context, id *httpctx.Identity, roleIDs []uuid.UUID) (bool, error) {
	var specs []store.GrantSpec
	for _, rid := range roleIDs {
		grants, err := s.store.RoleGrantsOnly(ctx, rid)
		if err != nil {
			return false, err
		}
		specs = append(specs, grantsToSpecs(grants)...)
	}
	return s.specsWithinCaller(ctx, id, specs)
}

// outranks reports whether the caller holds every right the target user has,
// the bar for resetting their credentials, revoking their sessions, changing
// or deleting them. Acting on yourself is always within your own rights.
func (s *Server) outranks(ctx context.Context, id *httpctx.Identity, target uuid.UUID) (bool, error) {
	if target == id.User.ID {
		return true, nil
	}
	theirs, err := s.store.UserGrants(ctx, target)
	if err != nil {
		return false, err
	}
	return s.specsWithinCaller(ctx, id, grantsToSpecs(theirs))
}
