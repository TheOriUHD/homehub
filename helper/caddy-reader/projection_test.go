package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A config shaped like the real thing, carrying the secrets a real one carries:
// a basic_auth hash, a credential on an upstream header, and an ACME EAB key.
const configWithSecrets = `{
  "admin": {"listen": "127.0.0.1:2019"},
  "apps": {
    "tls": {"automation": {"policies": [{"issuers": [{"eab": {"mac_key": "acme-eab-secret"}}]}]}},
    "http": {
      "servers": {
        "srv0": {
          "listen": [":443"],
          "routes": [
            {
              "match": [{"host": ["tracker.example.com"]}],
              "handle": [{
                "handler": "subroute",
                "routes": [{"handle": [{
                  "handler": "reverse_proxy",
                  "upstreams": [{"dial": "10.0.0.5:8080"}],
                  "headers": {"request": {"set": {"X-Api-Key": ["upstream-secret-value"]}}}
                }]}]
              }]
            },
            {
              "match": [{"host": ["locked.example.com"]}],
              "handle": [
                {"handler": "authentication", "providers": {"http_basic": {"accounts": [
                  {"username": "alice", "password": "$2a$14$aVeryRealBcryptHash"}
                ]}}},
                {"handler": "reverse_proxy", "upstreams": [{"dial": "[fd00::1]:9000"}]}
              ]
            }
          ]
        }
      }
    }
  }
}`

func projectJSON(t *testing.T, raw string) string {
	t.Helper()
	var config any
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		t.Fatalf("fixture is not JSON: %v", err)
	}
	out, err := json.Marshal(ProjectConfig(config))
	if err != nil {
		t.Fatalf("could not encode projection: %v", err)
	}
	return string(out)
}

func TestProjectionDropsSecrets(t *testing.T) {
	got := projectJSON(t, configWithSecrets)

	for _, secret := range []string{
		"$2a$14$aVeryRealBcryptHash", // basic_auth password hash
		"upstream-secret-value",      // credential on a header going upstream
		"acme-eab-secret",            // ACME external account binding key
	} {
		if strings.Contains(got, secret) {
			t.Errorf("projection leaked %q", secret)
		}
	}
}

func TestProjectionKeepsWhatItIsFor(t *testing.T) {
	got := projectJSON(t, configWithSecrets)

	for _, want := range []string{
		"tracker.example.com",
		"10.0.0.5:8080",
		"locked.example.com",
		"[fd00::1]:9000", // IPv6 survives verbatim; splitting it is the parser's job
	} {
		if !strings.Contains(got, want) {
			t.Errorf("projection dropped %q, which devtracker needs", want)
		}
	}
}

func TestProjectionKeepsNesting(t *testing.T) {
	// The host matcher sits on the outer route and the upstream three levels
	// below it. Flattening would lose which domain owns which upstream.
	got := projectJSON(t, configWithSecrets)
	if !strings.Contains(got, `"handler":"subroute"`) {
		t.Error("subroute nesting was flattened")
	}
}

func TestProjectionReducesDynamicUpstreamsToPresence(t *testing.T) {
	got := projectJSON(t, `{"apps":{"http":{"servers":{"s":{"routes":[
      {"match":[{"host":["x.example.com"]}],
       "handle":[{"handler":"reverse_proxy",
                  "dynamic_upstreams":{"srv":{"name":"svc","credentials":"do-not-send"}}}]}
    ]}}}}}`)

	if strings.Contains(got, "do-not-send") {
		t.Error("dynamic_upstreams contents were sent")
	}
	// Presence must survive, so devtracker can still say "cannot be resolved".
	if !strings.Contains(got, "dynamic_upstreams") {
		t.Error("dynamic_upstreams presence was lost")
	}
}

func TestProjectionSurvivesMalformedInput(t *testing.T) {
	// Anything can appear here; nothing may panic.
	for _, raw := range []string{
		`null`, `[]`, `"a string"`, `42`,
		`{"apps":null}`,
		`{"apps":{"http":{"servers":"not-an-object"}}}`,
		`{"apps":{"http":{"servers":{"s":{"routes":[null,3,"x",{"handle":"nope"}]}}}}}`,
		`{"apps":{"http":{"servers":{"s":{"routes":[{"match":[null,{"host":[1,2]}]}]}}}}}`,
	} {
		var config any
		_ = json.Unmarshal([]byte(raw), &config)
		if _, err := json.Marshal(ProjectConfig(config)); err != nil {
			t.Errorf("could not encode projection of %s: %v", raw, err)
		}
	}
}

// --- the server ---------------------------------------------------------------

func testServer(t *testing.T, caddyBody string, caddyStatus int) (*server, *httptest.Server) {
	t.Helper()
	caddy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != caddyConfigPath {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(caddyStatus)
		_, _ = w.Write([]byte(caddyBody))
	}))
	t.Cleanup(caddy.Close)

	return &server{
		admin:  caddy.URL,
		token:  "a-token-long-enough-to-use",
		client: &http.Client{},
	}, caddy
}

func do(t *testing.T, s *server, method, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)
	return rec
}

func TestConfigNeedsTheToken(t *testing.T) {
	s, _ := testServer(t, configWithSecrets, 200)

	if got := do(t, s, "GET", "/config", "").Code; got != http.StatusUnauthorized {
		t.Errorf("no token: got %d, want 401", got)
	}
	if got := do(t, s, "GET", "/config", "wrong-token-entirely").Code; got != http.StatusUnauthorized {
		t.Errorf("wrong token: got %d, want 401", got)
	}
	if got := do(t, s, "GET", "/config", s.token).Code; got != http.StatusOK {
		t.Errorf("correct token: got %d, want 200", got)
	}
}

func TestEveryWriteIsRefused(t *testing.T) {
	s, _ := testServer(t, configWithSecrets, 200)

	// Even holding a valid token. Read-only is a property of the program, not a
	// permission that a credential can unlock.
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		for _, path := range []string{"/config", "/health", "/load", "/"} {
			rec := do(t, s, method, path, s.token)
			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s: got %d, want 405", method, path, rec.Code)
			}
		}
	}
}

func TestUnknownPathsAreNotForwarded(t *testing.T) {
	s, _ := testServer(t, configWithSecrets, 200)

	// The admin API has many endpoints. None of them is reachable through here,
	// because the path is never taken from the request.
	for _, path := range []string{"/load", "/id/foo", "/reverse_proxy/upstreams", "/stop"} {
		if got := do(t, s, "GET", path, s.token).Code; got != http.StatusNotFound {
			t.Errorf("GET %s: got %d, want 404", path, got)
		}
	}
}

func TestTrailingSlashIsAccepted(t *testing.T) {
	s, _ := testServer(t, configWithSecrets, 200)
	if got := do(t, s, "GET", "/config/", s.token).Code; got != http.StatusOK {
		t.Errorf("/config/: got %d, want 200", got)
	}
	if got := do(t, s, "GET", "/health/", "").Code; got != http.StatusOK {
		t.Errorf("/health/: got %d, want 200", got)
	}
}

func TestHealthNeedsNoTokenAndRevealsNothing(t *testing.T) {
	s, _ := testServer(t, configWithSecrets, 200)
	rec := do(t, s, "GET", "/health", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
	// A monitoring probe should not need a credential — and should not be a way
	// to read the configuration without one either.
	for _, secret := range []string{"tracker.example.com", "10.0.0.5", "$2a$14$"} {
		if strings.Contains(rec.Body.String(), secret) {
			t.Errorf("/health leaked %q", secret)
		}
	}
}

func TestRawModeIsReportedSoItCannotBeSilent(t *testing.T) {
	s, _ := testServer(t, configWithSecrets, 200)
	s.raw = true

	rec := do(t, s, "GET", "/config", s.token)
	var body struct {
		Projected bool `json:"projected"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if body.Projected {
		t.Error("raw mode claimed to be projected")
	}
	if !strings.Contains(rec.Body.String(), "upstream-secret-value") {
		t.Error("raw mode did not actually return the raw config")
	}
}

func TestCaddyFailuresAreReportedNotSwallowed(t *testing.T) {
	s, _ := testServer(t, "not json at all", 200)
	rec := do(t, s, "GET", "/config", s.token)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("got %d, want 502", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "not Caddy's admin API") {
		t.Errorf("unhelpful message: %s", rec.Body.String())
	}
}

func TestHealthSaysDegradedWhenCaddyIsDown(t *testing.T) {
	s, caddy := testServer(t, configWithSecrets, 200)
	caddy.Close() // Caddy stops; the helper keeps running.

	rec := do(t, s, "GET", "/health", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("got %d, want 503", rec.Code)
	}
	// The distinction that matters: this helper is fine, Caddy is not.
	if !strings.Contains(rec.Body.String(), "degraded") {
		t.Errorf("did not report degraded: %s", rec.Body.String())
	}
}
