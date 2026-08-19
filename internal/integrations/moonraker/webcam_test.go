package moonraker

import "testing"

func TestResolveWebcamURL(t *testing.T) {
	cases := []struct {
		base string
		raw  string
		want string
	}{
		// Relative path: resolve against the host on its default web port,
		// dropping Moonraker's own port (the webcam lives behind nginx on 80).
		{"http://printer.local:7125", "/webcam/?action=stream", "http://printer.local/webcam/?action=stream"},
		{"http://printer.local:7125", "webcam/?action=stream", "http://printer.local/webcam/?action=stream"},
		{"http://printer.local", "/webcam/?action=stream", "http://printer.local/webcam/?action=stream"},
		{"https://printer.example.com:7125", "/webcam/?action=snapshot", "https://printer.example.com/webcam/?action=snapshot"},
		// Absolute URL on the printer's OWN host (a direct streamer port) is kept.
		{"http://printer.local:7125", "http://printer.local:8080/?action=stream", "http://printer.local:8080/?action=stream"},
		// SSRF guard: an absolute URL to a DIFFERENT host is refused.
		{"http://printer.local", "https://cam.example.com/stream", ""},
		{"http://printer.local:7125", "http://169.254.169.254/latest/meta-data/", ""},
		// Non-http scheme refused.
		{"http://printer.local:7125", "file:///etc/passwd", ""},
		// Empty stays empty.
		{"http://printer.local:7125", "", ""},
	}
	for _, c := range cases {
		conn := &Conn{client: &client{base: c.base}}
		if got := conn.resolveWebcamURL(c.raw); got != c.want {
			t.Errorf("resolveWebcamURL(base=%q, raw=%q) = %q, want %q", c.base, c.raw, got, c.want)
		}
	}
}
