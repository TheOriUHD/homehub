package notices

import (
	"testing"
	"time"
)

func berlin(t *testing.T) *time.Location {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skip("no zoneinfo")
	}
	return loc
}

func TestDigestDueFindsInstantsBetweenChecks(t *testing.T) {
	loc := berlin(t)
	p := Default("Europe/Berlin").Normalized()
	// Thursday 4 Sep 2026: weekday times 08:00 and 18:30.
	last := time.Date(2026, 9, 4, 18, 29, 30, 0, loc)
	now := time.Date(2026, 9, 4, 18, 30, 20, 0, loc)
	if !p.DigestDue(last, now) {
		t.Fatal("18:30 lies between the checks")
	}
	if p.DigestDue(now, now.Add(time.Minute)) {
		t.Fatal("nothing scheduled in the next minute")
	}
	// Saturday uses the weekend list: 10:00, not 08:00.
	sat0759 := time.Date(2026, 9, 5, 7, 59, 0, 0, loc)
	if p.DigestDue(sat0759, sat0759.Add(2*time.Minute)) {
		t.Fatal("08:00 is a weekday time, not a Saturday one")
	}
	sat0959 := time.Date(2026, 9, 5, 9, 59, 0, 0, loc)
	if !p.DigestDue(sat0959, sat0959.Add(2*time.Minute)) {
		t.Fatal("10:00 is the Saturday time")
	}
	// A gap across midnight still catches the morning slot.
	if !p.DigestDue(time.Date(2026, 9, 3, 23, 0, 0, 0, loc), time.Date(2026, 9, 4, 8, 0, 30, 0, loc)) {
		t.Fatal("08:00 the next morning lies in the gap")
	}
}

func TestPerDayModeAndNext(t *testing.T) {
	loc := berlin(t)
	p := Default("Europe/Berlin")
	p.Digest.Mode = ModePerDay
	p.Digest.Days = map[string][]string{"fri": {"12:00"}, "sun": {"20:00", "09:00"}}
	p = p.Normalized()
	if got := p.TimesFor(time.Sunday); len(got) != 2 || got[0] != "09:00" {
		t.Fatalf("per-day times should be sorted: %v", got)
	}
	if got := p.TimesFor(time.Monday); len(got) != 0 {
		t.Fatalf("Monday has no digest in this policy: %v", got)
	}
	next, ok := p.NextDigest(time.Date(2026, 9, 4, 12, 1, 0, 0, loc)) // Friday just after noon
	if !ok || next.Weekday() != time.Sunday || next.Hour() != 9 {
		t.Fatalf("next should be Sunday 09:00, got %v ok=%v", next, ok)
	}
}

func TestQuietHoursWrapMidnight(t *testing.T) {
	loc := berlin(t)
	p := Default("Europe/Berlin").Normalized() // 22:30 → 07:00
	if !p.InQuietHours(time.Date(2026, 9, 4, 23, 15, 0, 0, loc)) || !p.InQuietHours(time.Date(2026, 9, 5, 6, 59, 0, 0, loc)) {
		t.Fatal("late evening and early morning are quiet")
	}
	if p.InQuietHours(time.Date(2026, 9, 4, 7, 0, 0, 0, loc)) || p.InQuietHours(time.Date(2026, 9, 4, 12, 0, 0, 0, loc)) {
		t.Fatal("07:00 and noon are not quiet")
	}
}

func TestRouting(t *testing.T) {
	loc := berlin(t)
	p := Default("Europe/Berlin").Normalized()
	day := time.Date(2026, 9, 4, 12, 0, 0, 0, loc)
	night := time.Date(2026, 9, 4, 23, 30, 0, 0, loc)
	if p.RouteFor("critical", day) != RouteImmediate || p.RouteFor("critical", night) != RouteImmediate {
		t.Fatal("critical interrupts, quiet or not, by default")
	}
	if p.RouteFor("warning", day) != RouteDigest {
		t.Fatal("warnings wait for the digest by default")
	}
	p.Immediate["warning"] = true
	if p.RouteFor("warning", day) != RouteImmediate || p.RouteFor("warning", night) != RouteDigest {
		t.Fatal("an immediate warning still waits during quiet hours")
	}
	p.Quiet.CriticalDuring = false
	if p.RouteFor("critical", night) != RouteDigest {
		t.Fatal("critical can be kept quiet too")
	}
}

func TestValidateAndNormalize(t *testing.T) {
	p := Default("Europe/Berlin")
	p.Immediate = nil
	p.Digest.Weekdays = []string{"18:30", "08:00", "08:00", " 08:00 "}
	p = p.Normalized()
	if !p.Immediate["critical"] || len(p.Digest.Weekdays) != 2 || p.Digest.Weekdays[0] != "08:00" {
		t.Fatalf("normalize: %+v", p)
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("valid policy refused: %v", err)
	}
	p.Digest.Weekends = []string{"25:00"}
	if err := p.Validate(); err == nil {
		t.Fatal("25:00 must be refused")
	}
	p = Default("Mars/Olympus")
	if err := p.Validate(); err == nil {
		t.Fatal("unknown zone must be refused")
	}
}
