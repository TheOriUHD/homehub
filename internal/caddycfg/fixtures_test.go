package caddycfg

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func parseFile(t *testing.T, name string) *Result {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	res, err := Parse(raw)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return res
}

// The caddy-reader helper sends a projection (allowlist) of the running
// config, not the config itself. These fixtures are real `caddy adapt`
// output and the projection the shipped helper binary produced for it. If
// our parser reads a field the projection drops, a domain silently goes
// missing on import — so full config and projection must parse identically.
func TestProjectionAgreesWithFullConfig(t *testing.T) {
	full := parseFile(t, "caddy-adapted.json")
	projected := parseFile(t, "caddy-adapted.projected.json")

	if !reflect.DeepEqual(full.Upstreams, projected.Upstreams) {
		t.Errorf("upstreams diverge:\nfull:      %+v\nprojected: %+v", full.Upstreams, projected.Upstreams)
	}
	if !reflect.DeepEqual(full.DomainsWithoutProxy, projected.DomainsWithoutProxy) {
		t.Errorf("domainsWithoutProxy diverge:\nfull:      %v\nprojected: %v", full.DomainsWithoutProxy, projected.DomainsWithoutProxy)
	}
	if !reflect.DeepEqual(full.Unresolvable, projected.Unresolvable) {
		t.Errorf("unresolvable diverge:\nfull:      %+v\nprojected: %+v", full.Unresolvable, projected.Unresolvable)
	}
	// A warning on the projected config means we want a field the
	// projection drops — exactly the drift these fixtures exist to catch.
	if len(projected.Warnings) > 0 {
		t.Errorf("projected config produced warnings (parser wants a projected-away field): %v", projected.Warnings)
	}
	if len(full.Upstreams) == 0 {
		t.Fatal("fixture yielded zero upstreams — the test is vacuous")
	}
}

// The secrets fixture holds values that must never cross the network; the
// projection removes them. Both sides must still resolve the same routes,
// and the secret material must be absent from the projected file at all.
func TestSecretsProjectionParsesEqually(t *testing.T) {
	full := parseFile(t, "secrets-input.json")
	projected := parseFile(t, "secrets-input.projected.json")

	if !reflect.DeepEqual(full.Upstreams, projected.Upstreams) {
		t.Errorf("upstreams diverge:\nfull:      %+v\nprojected: %+v", full.Upstreams, projected.Upstreams)
	}
	if len(projected.Warnings) > 0 {
		t.Errorf("projected secrets config produced warnings: %v", projected.Warnings)
	}

	raw, err := os.ReadFile("testdata/secrets-input.projected.json")
	if err != nil {
		t.Fatal(err)
	}
	// The fixture's sentinel secret values (a bcrypt hash, upstream
	// credentials, an HMAC key). "secret.example.com" as a DOMAIN survives
	// on purpose — domains are the point — so match values, not substrings
	// like "secret".
	for _, needle := range []string{"$2a$14$aVeryRealBcryptHash", "must-not-appear", "secret-hmac"} {
		if strings.Contains(string(raw), needle) {
			t.Errorf("projected secrets fixture still contains %q", needle)
		}
	}
}
