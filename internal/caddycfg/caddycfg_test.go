package caddycfg

import (
	"testing"
)

// fixture in the shape `caddy adapt` produces: hosts on outer routes, the
// handlers nested inside subroutes, arbitrary server names, and one of each
// unresolvable kind.
const fixture = `{
  "apps": {
    "http": {
      "servers": {
        "frontend": {
          "listen": [":443"],
          "routes": [
            {
              "match": [{"host": ["app.example.com", "www.app.example.com"]}],
              "handle": [
                {
                  "handler": "subroute",
                  "routes": [
                    {
                      "handle": [
                        {"handler": "reverse_proxy", "upstreams": [{"dial": "10.0.0.5:8443"}]}
                      ]
                    }
                  ]
                }
              ]
            },
            {
              "match": [{"host": ["files.example.com"]}],
              "handle": [
                {"handler": "subroute", "routes": [
                  {"handle": [{"handler": "file_server"}]}
                ]}
              ]
            },
            {
              "match": [{"host": ["dyn.example.com"]}],
              "handle": [
                {"handler": "reverse_proxy", "dynamic_upstreams": {"source": "a"}}
              ]
            },
            {
              "match": [{"host": ["ph.example.com"]}],
              "handle": [
                {"handler": "reverse_proxy", "upstreams": [{"dial": "{env.UPSTREAM}"}]}
              ]
            },
            {
              "match": [{"host": ["sock.example.com"]}],
              "handle": [
                {"handler": "reverse_proxy", "upstreams": [{"dial": "unix//run/app.sock"}]}
              ]
            },
            {
              "match": [{"host": ["v6.example.com"]}],
              "handle": [
                {"handler": "reverse_proxy", "upstreams": [{"dial": "[fd00::1]:5000"}]}
              ]
            }
          ]
        },
        "catchall": {
          "listen": [":80"],
          "routes": [
            {"handle": [{"handler": "reverse_proxy", "upstreams": [{"dial": "127.0.0.1:8080"}]}]}
          ]
        }
      }
    }
  }
}`

func TestParseNestedSubroutes(t *testing.T) {
	res, err := Parse([]byte(fixture))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	byDomain := map[string]Upstream{}
	for _, u := range res.Upstreams {
		byDomain[u.Domain] = u
	}
	app, ok := byDomain["app.example.com"]
	if !ok || app.Host != "10.0.0.5" || app.Port != "8443" {
		t.Fatalf("app upstream missing or wrong (subroute walk broken): %+v", res.Upstreams)
	}
	if _, ok := byDomain["www.app.example.com"]; !ok {
		t.Fatal("second host in the matcher must also be reported")
	}
	v6 := byDomain["v6.example.com"]
	if v6.Host != "fd00::1" || v6.Port != "5000" {
		t.Fatalf("ipv6 dial parsed wrong: %+v", v6)
	}

	reasons := map[string]string{}
	for _, u := range res.Unresolvable {
		reasons[u.Domain] = u.Reason
	}
	if reasons["dyn.example.com"] != "upstreams are chosen dynamically at request time" {
		t.Fatalf("dynamic: %q", reasons["dyn.example.com"])
	}
	if reasons["ph.example.com"] != "address is a placeholder: {env.UPSTREAM}" {
		t.Fatalf("placeholder: %q", reasons["ph.example.com"])
	}
	if reasons["sock.example.com"] != "unix socket: unix//run/app.sock" {
		t.Fatalf("unix: %q", reasons["sock.example.com"])
	}

	if len(res.DomainsWithoutProxy) != 1 || res.DomainsWithoutProxy[0] != "files.example.com" {
		t.Fatalf("domains without proxy: %+v", res.DomainsWithoutProxy)
	}

	// The catchall reverse_proxy has no host matcher: warning, no upstream.
	foundWarning := false
	for _, w := range res.Warnings {
		if w == "a reverse_proxy has no host matcher — it answers for any domain and was skipped" {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Fatalf("expected the no-host-matcher warning, got %+v", res.Warnings)
	}
}

func TestParseDial(t *testing.T) {
	cases := []struct{ in, host, port string }{
		{"10.0.0.5:8080", "10.0.0.5", "8080"},
		{"h2c://10.0.0.5:8080", "10.0.0.5", "8080"},
		{"[::1]:8080", "::1", "8080"},
		{"localhost", "localhost", ""},
		{"fd00::1", "fd00::1", ""},
	}
	for _, c := range cases {
		h, p := ParseDial(c.in)
		if h != c.host || p != c.port {
			t.Fatalf("ParseDial(%q) = %q,%q want %q,%q", c.in, h, p, c.host, c.port)
		}
	}
}

func TestParseRejectsNonJSON(t *testing.T) {
	if _, err := Parse([]byte("app.example.com {\n}\n")); err == nil {
		t.Fatal("Caddyfile text must be rejected with the adapt hint")
	}
}
