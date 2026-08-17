package assistant

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

// fakeCall records the request and returns a scripted response.
type fakeCall struct {
	method, path string
	body         any
	status       int
	response     string
}

func (f *fakeCall) fn(_ context.Context, method, path string, body any) (int, []byte) {
	f.method, f.path, f.body = method, path, body
	return f.status, []byte(f.response)
}

func TestActionToolsBuildCorrectRequests(t *testing.T) {
	cases := []struct {
		tool     string
		input    string
		wantPath string
		wantBody map[string]any
	}{
		{"ha_call_service",
			`{"systemId":"11111111-1111-1111-1111-111111111111","entityId":"light.philipp","service":"turn_on","data":{"brightness_pct":50}}`,
			"/ha/11111111-1111-1111-1111-111111111111/entities/light.philipp/service",
			map[string]any{"service": "turn_on", "data": map[string]any{"brightness_pct": float64(50)}}},
		{"docker_container_action",
			`{"systemId":"11111111-1111-1111-1111-111111111111","name":"web-app","action":"restart"}`,
			"/docker/11111111-1111-1111-1111-111111111111/containers/web-app/action",
			map[string]any{"action": "restart"}},
		{"mc_command",
			`{"systemId":"11111111-1111-1111-1111-111111111111","command":"/say hi"}`,
			"/mc/11111111-1111-1111-1111-111111111111/command",
			map[string]any{"command": "say hi"}},
		{"amp_instance_action",
			`{"systemId":"11111111-1111-1111-1111-111111111111","instanceId":"i9","action":"start"}`,
			"/amp/11111111-1111-1111-1111-111111111111/instances/i9/lifecycle",
			map[string]any{"action": "start"}},
		{"pve_guest_action",
			`{"systemId":"11111111-1111-1111-1111-111111111111","vmid":104,"action":"shutdown"}`,
			"/proxmox/11111111-1111-1111-1111-111111111111/guests/104/action",
			map[string]any{"action": "shutdown"}},
		{"dns_blocking",
			`{"systemId":"11111111-1111-1111-1111-111111111111","enabled":false,"minutes":30}`,
			"/dns/11111111-1111-1111-1111-111111111111/blocking",
			map[string]any{"enabled": false, "minutes": 30}},
	}
	for _, tc := range cases {
		fake := &fakeCall{status: http.StatusOK, response: `{"ok":true}`}
		tb := &Toolbox{Call: fake.fn}
		out, err := tb.Run(context.Background(), Access{}, tc.tool, json.RawMessage(tc.input))
		if err != nil {
			t.Errorf("%s: %v", tc.tool, err)
			continue
		}
		if out == "" || fake.method != http.MethodPost || fake.path != tc.wantPath {
			t.Errorf("%s: got %s %s", tc.tool, fake.method, fake.path)
		}
		gotRaw, _ := json.Marshal(fake.body)
		wantRaw, _ := json.Marshal(tc.wantBody)
		if string(gotRaw) != string(wantRaw) {
			t.Errorf("%s body: got %s want %s", tc.tool, gotRaw, wantRaw)
		}
	}
}

// A refusal from the handler (403) must come back as a readable error the
// model can relay, with the handler's own message.
func TestCallSurfacesHandlerRefusal(t *testing.T) {
	fake := &fakeCall{status: http.StatusForbidden, response: `{"code":"forbidden","message":"you do not have permission to do that"}`}
	tb := &Toolbox{Call: fake.fn}
	_, err := tb.Run(context.Background(), Access{}, "mc_command",
		json.RawMessage(`{"systemId":"11111111-1111-1111-1111-111111111111","command":"stop"}`))
	if err == nil || err.Error() != "you do not have permission to do that" {
		t.Fatalf("refusal not surfaced: %v", err)
	}
}

func TestToolCountIncludesSystemTools(t *testing.T) {
	bare := (&Toolbox{}).Defs()
	full := (&Toolbox{Call: (&fakeCall{status: 200, response: "{}"}).fn}).Defs()
	if len(full) != len(bare)+8 {
		t.Fatalf("expected 8 system tools, got %d extra", len(full)-len(bare))
	}
	for _, def := range full {
		var parsed map[string]any
		if err := json.Unmarshal(def.InputSchema, &parsed); err != nil {
			t.Errorf("tool %s schema invalid: %v", def.Name, err)
		}
	}
}
