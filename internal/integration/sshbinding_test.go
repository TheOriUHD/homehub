package integration

import "testing"

// A config with no binding must not touch the store or the vault at all —
// nil handles here prove the early return happens before either is used.
func TestResolveSSHBindingUnbound(t *testing.T) {
	for _, config := range []map[string]any{
		nil,
		{},
		{"host": "192.168.1.5"},
		{SSHHostIDKey: ""},
	} {
		binding, bound, err := ResolveSSHBinding(t.Context(), nil, nil, config)
		if err != nil || bound || binding != (SSHBinding{}) {
			t.Fatalf("config %v: got (%v, %v, %v), want zero/false/nil", config, binding, bound, err)
		}
	}
}

func TestResolveSSHBindingMalformed(t *testing.T) {
	_, bound, err := ResolveSSHBinding(t.Context(), nil, nil, map[string]any{SSHHostIDKey: "not-a-uuid"})
	if !bound {
		t.Fatal("a malformed id is still a binding — reporting unbound would fall back to inline credentials")
	}
	if err == nil {
		t.Fatal("want an error for a malformed host id")
	}
}

func TestApplySSHBinding(t *testing.T) {
	original := map[string]any{
		"transport": "ssh",
		"host":      "stale.example",
		"username":  "stale",
		"port":      float64(2222),
		"authType":  "password",
	}
	out := ApplySSHBinding(original, SSHBinding{
		Hostname: "10.0.0.9", Port: 22, Username: "deploy", AuthType: "key", Secret: "s3cret",
	})

	if out["host"] != "10.0.0.9" || out["username"] != "deploy" || out["authType"] != "key" {
		t.Fatalf("binding did not overwrite the inline fields: %v", out)
	}
	// Driver config parsers type-assert float64, the shape JSON decoding gives.
	if port, ok := out["port"].(float64); !ok || port != 22 {
		t.Fatalf("port = %#v, want float64(22)", out["port"])
	}
	if out["transport"] != "ssh" {
		t.Fatal("unrelated config keys must survive")
	}
	if original["host"] != "stale.example" {
		t.Fatal("the caller's map was mutated — instance configs are shared")
	}
}
