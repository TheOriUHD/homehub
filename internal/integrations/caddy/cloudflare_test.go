package caddy

import "testing"

func TestCFNormalizeName(t *testing.T) {
	zone := "example.com"
	cases := []struct{ in, want string }{
		{"@", "example.com"},
		{"", "example.com"},
		{"example.com", "example.com"},
		{"amp", "amp.example.com"},
		{"AMP", "amp.example.com"},
		{"amp.example.com", "amp.example.com"},
		{"amp.example.com.", "amp.example.com"},
		{"deep.sub", "deep.sub.example.com"},
		{"*", "*.example.com"},
	}
	for _, c := range cases {
		if got := CFNormalizeName(c.in, zone); got != c.want {
			t.Errorf("CFNormalizeName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCFAutoType(t *testing.T) {
	cases := []struct{ in, want string }{
		{"192.168.10.67", "A"},
		{"2001:db8::1", "AAAA"},
		{"myhost.ddns.net", "CNAME"},
		{"caddy.local", "CNAME"},
	}
	for _, c := range cases {
		if got := CFAutoType(c.in); got != c.want {
			t.Errorf("CFAutoType(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
