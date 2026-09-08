package api

import (
	"bytes"
	"encoding/json"
	"testing"

	"homehub/internal/store"
)

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

// Most pins have nothing pointing at them by name. A nil slice marshals to
// null, the schema promises an array, and the page read .length off it — so
// the whole screen went blank on the addresses that are hardest to identify.
func TestKnownHostRowNeverShipsANullList(t *testing.T) {
	row := knownHostRow(store.SSHKnownHost{Host: "192.168.20.159", Port: 22, Fingerprint: "SHA256:x"}, nil)
	if row.UsedBy == nil {
		t.Fatal("usedBy is nil and would marshal to null")
	}
	raw, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(`"usedBy":null`)) {
		t.Fatalf("shipped a null list: %s", raw)
	}
	named := knownHostRow(store.SSHKnownHost{Host: "192.168.20.73", Port: 22}, []string{"Minecraft VM"})
	if len(named.UsedBy) != 1 || named.UsedBy[0] != "Minecraft VM" {
		t.Fatalf("names were dropped: %v", named.UsedBy)
	}
}
