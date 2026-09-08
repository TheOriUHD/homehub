package api

import "testing"

// The address a request names decides which pin is replaced, so a malformed
// one has to be refused rather than defaulted into something plausible.
func TestHostKeyRefNormalisesAnAddress(t *testing.T) {
	port := func(n int) *int { return &n }

	if h, p, err := hostKeyRef("  192.168.1.5  ", nil); err != nil || h != "192.168.1.5" || p != 22 {
		t.Fatalf("got (%q,%d,%v), want the trimmed host on the default port", h, p, err)
	}
	if _, p, err := hostKeyRef("box", port(2222)); err != nil || p != 2222 {
		t.Fatalf("an explicit port was not kept: %d %v", p, err)
	}
	for _, bad := range []struct {
		host string
		port *int
	}{
		{"", nil},
		{"   ", nil},
		{"box", port(0)},
		{"box", port(-1)},
		{"box", port(70000)},
	} {
		if _, _, err := hostKeyRef(bad.host, bad.port); err == nil {
			t.Errorf("accepted host=%q port=%v", bad.host, bad.port)
		}
	}
}
