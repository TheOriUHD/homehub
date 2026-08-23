package signals

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// A bouncing connection produced a day of alternating "it is down" / "it is
// back" messages: every blip stopped the collector reporting, which
// auto-resolved the signal and pushed an all-clear, and the next sweep raised
// it again. The dwell windows make the condition survive the bounce.
func TestStatusCollectorRidesOutFlapping(t *testing.T) {
	id := uuid.New()
	c := &integrationStatusCollector{}
	now := time.Now()

	// A fault younger than alertDwell is not reported at all — most blips are
	// gone before the next sweep.
	if _, ok := c.heldFault(id, now); ok {
		t.Fatal("nothing should be held before anything was seen")
	}

	// Once it has held long enough it is reported, and remembered.
	obs := Observation{Key: "integ:" + id.String() + ":status", Severity: SevCritical, Title: "down"}
	c.remember(id, obs, now)

	// It now looks healthy again — but only for a moment. The fault must stay
	// open, or the engine resolves it and pushes an all-clear it will
	// contradict on the next sweep.
	held, ok := c.heldFault(id, now.Add(2*time.Minute))
	if !ok || held.Key != obs.Key {
		t.Fatal("a fault must survive a brief recovery")
	}
	if _, ok := c.heldFault(id, now.Add(recoveryDwell-time.Minute)); !ok {
		t.Error("still inside the recovery window — the fault should hold")
	}

	// Once the recovery has actually stuck, it clears and the engine is free
	// to resolve it and send one all-clear.
	if _, ok := c.heldFault(id, now.Add(recoveryDwell+time.Minute)); ok {
		t.Error("a recovery that held for the full window must be believed")
	}
	if _, ok := c.heldFault(id, now.Add(recoveryDwell+2*time.Minute)); ok {
		t.Error("the fault should have been forgotten, not re-held")
	}
}

func TestStatusDwellWindowsAreSane(t *testing.T) {
	if alertDwell >= recoveryDwell {
		t.Error("recovery must be slower to believe than a fault, or flapping still gets through")
	}
	if alertDwell < time.Minute {
		t.Error("the dwell must exceed the sweep interval or it filters nothing")
	}
}
