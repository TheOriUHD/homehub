package assistant

import "testing"

// The routing table is the product promise: cheap for errands, capable for
// interpretation, and predictable enough to state in a sentence. These cases
// are written as the user would type them.
func TestClassifyLaneKeepsErrandsCheap(t *testing.T) {
	basic := []string{
		"turn on the kitchen light",
		"turn off light.living_room_lamp",
		"is everything healthy?",
		"how full is the NAS?",
		"what alerts fired today",
		"restart the frigate container",
		"list my proxmox guests",
		"how many players are online",
		"set the thermostat to 21",
		"status",
		"mach das licht im wohnzimmer an",
	}
	for _, msg := range basic {
		if r := ClassifyLane(msg); r.Lane != LaneBasic {
			t.Errorf("%q escalated to %s via %q — errands should stay cheap", msg, r.Lane, r.Rule)
		}
	}
}

func TestClassifyLaneEscalatesInterpretation(t *testing.T) {
	cases := map[string]string{
		"but WHY is it like that":                                    "asks why",
		"why is aperture degraded":                                   "asks why",
		"what's the truth that Signals alone doesn't tell me":        "challenges the answer",
		"are you sure? that doesn't match what I see":                "challenges the answer",
		"should I move the VM to the other node":                     "asks for judgement",
		"compare the two nodes and tell me which one is healthier":   "asks for judgement",
		"investigate the recording loop and get to the bottom of it": "asks for diagnosis",
		"walk me through fixing the backup job":                      "asks to plan or write",
	}
	for msg, wantRule := range cases {
		r := ClassifyLane(msg)
		if r.Lane != LaneAdvanced {
			t.Errorf("%q stayed basic — it needs interpretation", msg)
			continue
		}
		if r.Rule != wantRule {
			t.Errorf("%q escalated via %q, expected %q", msg, r.Rule, wantRule)
		}
	}
}

func TestClassifyLaneEscalatesLongQuestions(t *testing.T) {
	long := ""
	for i := 0; i < longMessageWords; i++ {
		long += "context "
	}
	if r := ClassifyLane(long); r.Lane != LaneAdvanced || r.Rule != "long question" {
		t.Errorf("a %d-word message stayed basic: %+v", longMessageWords, r)
	}
	if r := ClassifyLane("check disk space"); r.Lane != LaneBasic {
		t.Errorf("a short message escalated: %+v", r)
	}
}

// Empty text arrives on regeneration, before the parent message is read.
func TestClassifyLaneEmpty(t *testing.T) {
	if r := ClassifyLane("   "); r.Lane != LaneBasic || r.Rule != "" {
		t.Errorf("empty text should fall to basic with no rule, got %+v", r)
	}
}

func TestLaneModelPrefersConfiguredOverrides(t *testing.T) {
	// Catalog providers resolve from the registry.
	if got := LaneModel("anthropic", LaneBasic, "", ""); got != "claude-haiku-4-5" {
		t.Errorf("anthropic basic = %q", got)
	}
	if got := LaneModel("anthropic", LaneAdvanced, "", ""); got != "claude-sonnet-5" {
		t.Errorf("anthropic advanced = %q", got)
	}
	if got := LaneModel("openai", LaneBasic, "", ""); got != "gpt-5.6-luna" {
		t.Errorf("openai basic = %q", got)
	}
	// An administrator's own pair wins, and is the only source for a
	// catalog-less provider.
	if got := LaneModel("ollama", LaneAdvanced, "qwen3:8b", "qwen3:32b"); got != "qwen3:32b" {
		t.Errorf("local advanced = %q", got)
	}
	if AutoAvailable("ollama", "", "") {
		t.Error("Auto must not offer itself when no lanes are configured")
	}
	if !AutoAvailable("ollama", "qwen3:8b", "qwen3:32b") {
		t.Error("Auto should be available once both local lanes are set")
	}
	if !AutoAvailable("anthropic", "", "") {
		t.Error("Auto should be available for a catalog provider out of the box")
	}
}

// Auto must never reach for a model that is behind a spend confirmation —
// silently spending 5x because a message said "why" would defeat the gate.
func TestAutoNeverRoutesToAGatedModel(t *testing.T) {
	for _, id := range []string{"anthropic", "openai"} {
		for _, lane := range []Lane{LaneBasic, LaneAdvanced} {
			model := LaneModel(id, lane, "", "")
			info := ModelInfoFor(model)
			if info == nil {
				t.Fatalf("%s %s lane resolved to unknown model %q", id, lane, model)
			}
			if info.Tier != TierStandard {
				t.Errorf("%s %s lane routes to gated model %s", id, lane, model)
			}
		}
	}
}
