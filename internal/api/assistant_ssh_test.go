package api

import (
	"testing"

	"homehub/internal/sshguard"
)

// The execution path re-judges every assistant command on its own: an
// unattended run is for read-only commands only, whatever the caller above
// believed, and nothing — not even an approval — runs a catastrophic one.
func TestAssistantSSHGate(t *testing.T) {
	for _, tc := range []struct {
		command  string
		approved bool
		allowed  bool
	}{
		{"df -h", false, true},
		{"df -h", true, true},
		{"systemctl restart nginx", false, false},
		{"systemctl restart nginx", true, true},
		{"find / -name x -delete", false, false},
		{"rm -rf /", true, false},
		{"rm -rf /", false, false},
	} {
		err := assistantSSHGate(sshguard.Classify(tc.command), tc.approved)
		if (err == nil) != tc.allowed {
			t.Errorf("%q approved=%v: err=%v, want allowed=%v", tc.command, tc.approved, err, tc.allowed)
		}
	}
}
