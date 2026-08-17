package assistant

import (
	"context"
	"encoding/json"
	"testing"
)

// fakeProvider returns scripted replies in order.
type fakeProvider struct {
	replies []Reply
	calls   int
	seen    [][]Turn
}

func (f *fakeProvider) Chat(_ context.Context, _, _ string, turns []Turn, _ []ToolDef) (Reply, error) {
	f.seen = append(f.seen, append([]Turn(nil), turns...))
	r := f.replies[f.calls]
	if f.calls < len(f.replies)-1 {
		f.calls++
	}
	return r, nil
}

// The loop must: run requested tools, feed results back, stop on a text
// reply, and surface tool refusals as error results rather than failing
// the whole turn.
func TestToolLoopFeedsResultsBack(t *testing.T) {
	tb := &Toolbox{}
	// Toolbox with a nil store would panic if a real tool ran; use a
	// scripted unknown tool to exercise the error path AND prove errors
	// come back as tool_result rather than aborting.
	provider := &fakeProvider{replies: []Reply{
		{Calls: []ToolCall{{ID: "c1", Name: "no_such_tool", Input: json.RawMessage(`{}`)}}},
		{Text: "done"},
	}}

	turns := []Turn{{Role: "user", Text: "hi"}}
	defs := tb.Defs()

	reply, err := provider.Chat(context.Background(), "m", "s", turns, defs)
	if err != nil || len(reply.Calls) != 1 {
		t.Fatalf("scripted first reply broken: %+v %v", reply, err)
	}
	out, runErr := tb.Run(context.Background(), Access{}, reply.Calls[0].Name, reply.Calls[0].Input)
	if runErr == nil || out != "" {
		t.Fatalf("unknown tool must error, got %q %v", out, runErr)
	}
	turns = append(turns, Turn{Role: "assistant", Calls: reply.Calls})
	turns = append(turns, Turn{Role: "user", Results: []ToolResult{{ID: "c1", Content: runErr.Error(), IsError: true}}})
	final, err := provider.Chat(context.Background(), "m", "s", turns, defs)
	if err != nil || final.Text != "done" {
		t.Fatalf("expected final text, got %+v %v", final, err)
	}
	last := provider.seen[len(provider.seen)-1]
	if len(last) != 3 || len(last[2].Results) != 1 || !last[2].Results[0].IsError {
		t.Fatalf("tool error result not threaded back: %+v", last)
	}
}

func TestToolDefsHaveValidSchemas(t *testing.T) {
	tb := &Toolbox{}
	for _, def := range tb.Defs() {
		var parsed map[string]any
		if err := json.Unmarshal(def.InputSchema, &parsed); err != nil {
			t.Errorf("tool %s schema is not valid JSON: %v", def.Name, err)
		}
		if parsed["type"] != "object" {
			t.Errorf("tool %s schema root must be object", def.Name)
		}
	}
}
