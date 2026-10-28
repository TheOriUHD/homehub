package calendar

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"homehub/internal/store"
)

func TestValidateRRuleRefusesSubHourly(t *testing.T) {
	for rule, ok := range map[string]bool{
		"":                              true,
		"FREQ=DAILY":                    true,
		"RRULE:FREQ=WEEKLY;BYDAY=MO,WE": true,
		"FREQ=HOURLY;INTERVAL=2":        true,
		"FREQ=DAILY;BYHOUR=8,20":        true,
		"FREQ=SECONDLY":                 false,
		"FREQ=MINUTELY;INTERVAL=30":     false,
		"FREQ=HOURLY;BYMINUTE=0,30":     false,
		"FREQ=DAILY;BYHOUR=0,1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23;BYMINUTE=0,15": false,
		"FREQ=NONSENSE": false,
	} {
		if err := ValidateRRule(rule); (err == nil) != ok {
			t.Errorf("ValidateRRule(%q) = %v, want ok=%v", rule, err, ok)
		}
	}
}

// A synced feed can still carry a dense rule (validation only guards our own
// writes), so expansion must stay bounded on its own.
func TestExpandBoundsDenseRules(t *testing.T) {
	start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	rows := []store.CalendarEvent{{
		ID: uuid.New(), CalendarID: uuid.New(), UID: "dense",
		StartsAt: start, EndsAt: start.Add(time.Second), RRule: "FREQ=SECONDLY",
	}}
	began := time.Now()
	out := Expand(rows, start.AddDate(0, 6, 0), start.AddDate(1, 6, 0))
	if took := time.Since(began); took > 2*time.Second {
		t.Fatalf("expansion took %s", took)
	}
	if len(out) > maxInstancesPerEvent {
		t.Fatalf("got %d instances, cap is %d", len(out), maxInstancesPerEvent)
	}
}
