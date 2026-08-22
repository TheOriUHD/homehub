package assistant

import "testing"

// The figures behind the info tooltip. Wrong prices here are invisible until
// someone reconciles a bill, so pin the ratios the picker's warnings promise.
func TestEstimateCostMatchesTheAdvertisedRatios(t *testing.T) {
	u := Usage{InputTokens: 1_000_000, OutputTokens: 1_000_000}
	cost := func(model string) float64 {
		c, ok := EstimateCost(model, u)
		if !ok {
			t.Fatalf("no price for %s", model)
		}
		return c
	}
	sonnet, opus, fable := cost("claude-sonnet-5"), cost("claude-opus-5"), cost("claude-fable-5")

	// "About 2.5× the price of Sonnet 5" — the Opus gate's claim.
	if ratio := opus / sonnet; ratio < 2.4 || ratio > 2.6 {
		t.Errorf("Opus/Sonnet = %.2f, but the confirmation says about 2.5×", ratio)
	}
	// "Twice the price of Opus 5" — the Fable gate's claim.
	if ratio := fable / opus; ratio < 1.95 || ratio > 2.05 {
		t.Errorf("Fable/Opus = %.2f, but the confirmation says twice", ratio)
	}
	// A dated snapshot must price identically to the row it aliases.
	pinned, ok := EstimateCost("claude-haiku-4-5-20251001", u)
	if !ok || pinned != cost("claude-haiku-4-5") {
		t.Errorf("aliased Haiku snapshot priced differently: %v %v", pinned, ok)
	}
	// An unknown model reports "no price", never a silent zero.
	if _, ok := EstimateCost("qwen3:8b", u); ok {
		t.Error("a model with no published price must not be priced")
	}
}

// A worked example in the units the tooltip shows.
func TestEstimateCostWorkedExample(t *testing.T) {
	// 8.2k in + 420 out on Haiku 4.5 ($1 / $5 per MTok).
	got, _ := EstimateCost("claude-haiku-4-5", Usage{InputTokens: 8200, OutputTokens: 420})
	want := 8200/1e6*1.0 + 420/1e6*5.0 // = 0.0103
	if got != want {
		t.Fatalf("cost = %v, want %v", got, want)
	}
	if got < 0.0102 || got > 0.0104 {
		t.Fatalf("expected about $0.010, got %v", got)
	}
}
