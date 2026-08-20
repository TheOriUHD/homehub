package caddy

import "testing"

func TestCFNormalizeName(t *testing.T) {
	zone := "smarthomegup.de"
	cases := []struct{ in, want string }{
		{"@", "smarthomegup.de"},
		{"", "smarthomegup.de"},
		{"smarthomegup.de", "smarthomegup.de"},
		{"amp", "amp.smarthomegup.de"},
		{"AMP", "amp.smarthomegup.de"},
		{"amp.smarthomegup.de", "amp.smarthomegup.de"},
		{"amp.smarthomegup.de.", "amp.smarthomegup.de"},
		{"deep.sub", "deep.sub.smarthomegup.de"},
		{"*", "*.smarthomegup.de"},
	}
	for _, c := range cases {
		if got := CFNormalizeName(c.in, zone); got != c.want {
			t.Errorf("CFNormalizeName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCFAutoType(t *testing.T) {
	cases := []struct{ in, want string }{
		{"192.168.20.67", "A"},
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
