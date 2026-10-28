package assistant

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

const (
	ctlSys    = "11111111-1111-1111-1111-111111111111"
	ctlServer = "22222222-2222-2222-2222-222222222222"
)

// Controls that open the house or take a system down come back as a held
// proposal and send NOTHING to the API; benign ones run straight away.
func TestDangerousControlsWaitForApproval(t *testing.T) {
	coverList := `{"entities":[
		{"entityId":"cover.garage","attributes":{"device_class":"garage"}},
		{"entityId":"cover.living_blinds","attributes":{"device_class":"blind"}},
		{"entityId":"cover.mystery","attributes":{}}]}`
	for _, tc := range []struct {
		tool, input string
		held        bool
	}{
		{"ha_call_service", `{"systemId":"` + ctlSys + `","entityId":"lock.front_door","service":"unlock"}`, true},
		{"ha_call_service", `{"systemId":"` + ctlSys + `","entityId":"lock.front_door","service":"open"}`, true},
		{"ha_call_service", `{"systemId":"` + ctlSys + `","entityId":"lock.front_door","service":"lock"}`, false},
		{"ha_call_service", `{"systemId":"` + ctlSys + `","entityId":"alarm_control_panel.home","service":"alarm_disarm","data":{"code":"1234"}}`, true},
		{"ha_call_service", `{"systemId":"` + ctlSys + `","entityId":"alarm_control_panel.home","service":"alarm_arm_away"}`, false},
		{"ha_call_service", `{"systemId":"` + ctlSys + `","entityId":"cover.garage","service":"open_cover"}`, true},
		{"ha_call_service", `{"systemId":"` + ctlSys + `","entityId":"cover.garage","service":"toggle"}`, true},
		{"ha_call_service", `{"systemId":"` + ctlSys + `","entityId":"cover.mystery","service":"open_cover"}`, true},
		{"ha_call_service", `{"systemId":"` + ctlSys + `","entityId":"cover.living_blinds","service":"open_cover"}`, false},
		{"ha_call_service", `{"systemId":"` + ctlSys + `","entityId":"cover.garage","service":"close_cover"}`, false},
		{"ha_call_service", `{"systemId":"` + ctlSys + `","entityId":"light.kitchen","service":"turn_on"}`, false},
		{"docker_container_action", `{"systemId":"` + ctlSys + `","name":"web","action":"stop"}`, true},
		{"docker_container_action", `{"systemId":"` + ctlSys + `","name":"web","action":"restart"}`, true},
		{"docker_container_action", `{"systemId":"` + ctlSys + `","name":"web","action":"pause"}`, true},
		{"docker_container_action", `{"systemId":"` + ctlSys + `","name":"web","action":"start"}`, false},
		{"pve_guest_action", `{"systemId":"` + ctlSys + `","vmid":101,"action":"shutdown"}`, true},
		{"pve_guest_action", `{"systemId":"` + ctlSys + `","vmid":101,"action":"stop"}`, true},
		{"pve_guest_action", `{"systemId":"` + ctlSys + `","vmid":101,"action":"reboot"}`, true},
		{"pve_guest_action", `{"systemId":"` + ctlSys + `","vmid":101,"action":"start"}`, false},
		{"amp_instance_action", `{"systemId":"` + ctlSys + `","instanceId":"i1","action":"stop"}`, true},
		{"amp_instance_action", `{"systemId":"` + ctlSys + `","instanceId":"i1","action":"start"}`, false},
		{"mc_power", `{"systemId":"` + ctlSys + `","serverId":"` + ctlServer + `","action":"stop"}`, true},
		{"mc_power", `{"systemId":"` + ctlSys + `","serverId":"` + ctlServer + `","action":"start"}`, false},
		{"mc_command", `{"systemId":"` + ctlSys + `","serverId":"` + ctlServer + `","command":"op Steve"}`, true},
		{"mc_command", `{"systemId":"` + ctlSys + `","serverId":"` + ctlServer + `","command":"/minecraft:deop Steve"}`, true},
		{"mc_command", `{"systemId":"` + ctlSys + `","serverId":"` + ctlServer + `","command":"ban Alex griefing"}`, true},
		{"mc_command", `{"systemId":"` + ctlSys + `","serverId":"` + ctlServer + `","command":"stop"}`, true},
		{"mc_command", `{"systemId":"` + ctlSys + `","serverId":"` + ctlServer + `","command":"whitelist off"}`, true},
		{"mc_command", `{"systemId":"` + ctlSys + `","serverId":"` + ctlServer + `","command":"execute as @a run op @s"}`, true},
		{"mc_command", `{"systemId":"` + ctlSys + `","serverId":"` + ctlServer + `","command":"lp user Steve parent set admin"}`, true},
		{"mc_command", `{"systemId":"` + ctlSys + `","serverId":"` + ctlServer + `","command":"whitelist add Steve"}`, false},
		{"mc_command", `{"systemId":"` + ctlSys + `","serverId":"` + ctlServer + `","command":"give Steve diamond 3"}`, false},
	} {
		fake := &fakeCall{status: http.StatusOK, response: coverList}
		tb := &Toolbox{Call: fake.fn}
		out, err := tb.RunTool(context.Background(), Access{}, tc.tool, json.RawMessage(tc.input))
		if err != nil {
			t.Errorf("%s %s: %v", tc.tool, tc.input, err)
			continue
		}
		pa, held := out.(*PendingAction)
		if held != tc.held {
			t.Errorf("%s %s: held=%v, want %v", tc.tool, tc.input, held, tc.held)
			continue
		}
		for _, c := range fake.calls {
			if held && c.Method != http.MethodGet {
				t.Errorf("%s %s: held, yet sent %s %s", tc.tool, tc.input, c.Method, c.Path)
			}
		}
		if !held {
			continue
		}
		// The approval must rebuild exactly the request the tool would have
		// sent, from the params alone.
		req, err := ControlRequestFor(pa.Kind, pa.Params)
		if err != nil {
			t.Errorf("%s: held action does not rebuild: %v", tc.tool, err)
			continue
		}
		if req.Method != http.MethodPost || !strings.Contains(req.Path, ctlSys) {
			t.Errorf("%s: rebuilt %s %s", tc.tool, req.Method, req.Path)
		}
		if !IsControlKind(pa.Kind) || pa.Summary == "" {
			t.Errorf("%s: proposal kind %q summary %q", tc.tool, pa.Kind, pa.Summary)
		}
	}
}

// An approved HA call carries its service data intact — an alarm code that
// went missing would turn the approval into a silent failure.
func TestHeldControlKeepsItsData(t *testing.T) {
	fake := &fakeCall{status: http.StatusOK, response: `{}`}
	tb := &Toolbox{Call: fake.fn}
	out, err := tb.RunTool(context.Background(), Access{}, "ha_call_service",
		json.RawMessage(`{"systemId":"`+ctlSys+`","entityId":"alarm_control_panel.home","service":"alarm_disarm","data":{"code":"1234"}}`))
	if err != nil {
		t.Fatal(err)
	}
	pa := out.(*PendingAction)
	req, err := ControlRequestFor(pa.Kind, pa.Params)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(req.Body)
	if string(got) != `{"data":{"code":"1234"},"service":"alarm_disarm"}` {
		t.Errorf("rebuilt body = %s", got)
	}
	if req.Path != "/ha/"+ctlSys+"/entities/alarm_control_panel.home/service" {
		t.Errorf("rebuilt path = %s", req.Path)
	}
}
