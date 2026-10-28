package caddyfile

import (
	"strings"
	"testing"
)

func TestValidateEntryRefusesBreakouts(t *testing.T) {
	base := func() Entry {
		return Entry{Domains: []string{"app.example.com"}, Upstream: Upstream{Host: "192.168.1.5", Port: "8080"}}
	}
	if err := ValidateEntry(base()); err != nil {
		t.Fatalf("plain entry refused: %v", err)
	}
	ok := base()
	ok.Encode = "zstd gzip"
	ok.HeaderUp = []HeaderUp{{Name: "X-Real-IP", Value: "{remote_host}"}, {Name: "-Server", Value: "x"}}
	ok.DNSChallenge = &DNSChallenge{Provider: "cloudflare", Args: "{env.CLOUDFLARE_API_TOKEN}"}
	ok.Upstream.Host = "fe80::1%eth0"
	if err := ValidateEntry(ok); err != nil {
		t.Fatalf("legitimate entry refused: %v", err)
	}

	bad := map[string]func(*Entry){
		"newline in header value": func(e *Entry) { e.HeaderUp = []HeaderUp{{Name: "X", Value: "a\n}\nevil.com {\n\trespond 200"}} },
		"brace in header value":   func(e *Entry) { e.HeaderUp = []HeaderUp{{Name: "X", Value: "a }"}} },
		"comment in header value": func(e *Entry) { e.HeaderUp = []HeaderUp{{Name: "X", Value: "a # b"}} },
		"space in header name":    func(e *Entry) { e.HeaderUp = []HeaderUp{{Name: "X Y", Value: "a"}} },
		"directive in encode":     func(e *Entry) { e.Encode = "gzip\n\tbasicauth" },
		"unknown encoding":        func(e *Entry) { e.Encode = "gzip respond" },
		"newline in scheme":       func(e *Entry) { e.Upstream.Scheme = "http\nrespond" },
		"brace in upstream host":  func(e *Entry) { e.Upstream.Host = "a}" },
		"newline in dns args":     func(e *Entry) { e.DNSChallenge = &DNSChallenge{Provider: "cloudflare", Args: "x\n}"} },
		"bad dns provider":        func(e *Entry) { e.DNSChallenge = &DNSChallenge{Provider: "cloudflare }"} },
	}
	for name, mutate := range bad {
		e := base()
		mutate(&e)
		if err := ValidateEntry(e); err == nil {
			t.Errorf("%s: accepted, renders as:\n%s", name, strings.TrimSpace(RenderEntry(e)))
		}
	}
}
