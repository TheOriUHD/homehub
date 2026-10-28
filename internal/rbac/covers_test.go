package rbac

import (
	"testing"

	"homehub/internal/store"
)

func TestCovers(t *testing.T) {
	owner := []store.Grant{{Selector: "*", Levels: AllLevels, Effect: "allow"}}
	userAdmin := []store.Grant{
		{Selector: "*", Levels: []string{"view", "control"}, Effect: "allow"},
		{Selector: "system/users", Levels: []string{"view", "configure"}, Effect: "allow"},
		{Selector: "system/roles", Levels: []string{"view"}, Effect: "deny"},
	}
	dockerOnly := []store.Grant{{Selector: "docker/abc", Levels: AllLevels, Effect: "allow"}}

	cases := []struct {
		name     string
		holder   []store.Grant
		selector string
		levels   []string
		want     bool
	}{
		{"owner covers anything", owner, "*", AllLevels, true},
		{"control everywhere covers control on a host", userAdmin, "ssh/1", []string{"control"}, true},
		{"control does not cover configure", userAdmin, "ssh/1", []string{"configure"}, false},
		{"a deny inside the subtree blocks the whole selector", userAdmin, "*", []string{"view"}, false},
		{"a deny inside the subtree blocks the parent", userAdmin, "system", []string{"view"}, false},
		{"the granted node itself", userAdmin, "system/users", []string{"configure"}, true},
		{"narrow grant does not cover the wildcard", dockerOnly, "docker/*", []string{"view"}, false},
		{"narrow grant covers its own subtree", dockerOnly, "docker/abc/container/web", []string{"delete"}, true},
	}
	for _, c := range cases {
		if got := Covers(c.holder, c.selector, c.levels); got != c.want {
			t.Errorf("%s: Covers(%q, %v) = %v, want %v", c.name, c.selector, c.levels, got, c.want)
		}
	}

	ownerRole := []store.GrantSpec{{Selector: "*", Levels: AllLevels, Effect: "allow"}}
	if CoversAll(userAdmin, ownerRole) {
		t.Error("a user admin must not be able to hand out the Owner role")
	}
	if !CoversAll(userAdmin, []store.GrantSpec{{Selector: "*", Levels: AllLevels, Effect: "deny"}}) {
		t.Error("deny grants only take away and may always be handed out")
	}
}
