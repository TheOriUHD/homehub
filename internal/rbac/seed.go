package rbac

import (
	"context"
	"fmt"

	"github.com/TheOriUHD/homehub/internal/store"
)

// System role names. These exist in every installation and cannot be deleted;
// custom roles are built alongside them.
const (
	RoleOwner    = "Owner"
	RoleOperator = "Operator"
	RoleViewer   = "Viewer"
)

// EnsureSystemRoles creates the built-in roles when missing. Owner holds
// everything. Operator can see and control anything an admin connects but
// cannot reconfigure the system or manage identities. Viewer is read-only,
// also excluding system administration.
func EnsureSystemRoles(ctx context.Context, st *store.Store) error {
	systemRoles := []struct {
		name, description string
		grants            []store.GrantSpec
	}{
		{
			RoleOwner, "Full access to everything, including system administration.",
			[]store.GrantSpec{{Selector: "*", Levels: AllLevels, Effect: "allow"}},
		},
		{
			RoleOperator, "Can view and operate every connected system; cannot change configuration, manage users, or delete resources.",
			[]store.GrantSpec{
				{Selector: "*", Levels: []string{string(LevelView), string(LevelControl)}, Effect: "allow"},
				{Selector: "system", Levels: []string{string(LevelView)}, Effect: "deny"},
			},
		},
		{
			RoleViewer, "Read-only view of every connected system; no system administration.",
			[]store.GrantSpec{
				{Selector: "*", Levels: []string{string(LevelView)}, Effect: "allow"},
				{Selector: "system", Levels: []string{string(LevelView)}, Effect: "deny"},
			},
		},
	}

	for _, r := range systemRoles {
		_, err := st.GetRoleByName(ctx, r.name)
		if err == nil {
			continue
		}
		if err != store.ErrNotFound {
			return fmt.Errorf("check role %s: %w", r.name, err)
		}
		if _, err := st.CreateRole(ctx, r.name, r.description, true, r.grants); err != nil {
			return fmt.Errorf("create role %s: %w", r.name, err)
		}
	}
	return nil
}
