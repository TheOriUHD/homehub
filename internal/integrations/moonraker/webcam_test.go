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
		// Absolute URL (a direct streamer port) is used verbatim.
		{"http://printer.local:7125", "http://printer.local:8080/?action=stream", "http://printer.local:8080/?action=stream"},
		{"http://printer.local", "https://cam.example.com/stream", "https://cam.example.com/stream"},
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
