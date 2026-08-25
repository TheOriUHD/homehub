package api

import "testing"

func TestLoopbackURL(t *testing.T) {
	for raw, want := range map[string]bool{
		"http://127.0.0.1:8080":    true,
		"http://localhost":         true,
		"http://[::1]:8080":        true,
		"https://smarthomegup.de":  false,
		"http://192.168.20.5:8080": false,
		"":                         false,
	} {
		if got := loopbackURL(raw); got != want {
			t.Errorf("loopbackURL(%q) = %v, want %v", raw, got, want)
		}
	}
}
