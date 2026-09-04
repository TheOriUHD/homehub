// Package notices holds the owner's rules for how monitored trouble reaches
// their phone: what interrupts at once, what waits for a digest, when the
// digests go out, and when the house keeps quiet. Pure — no store, no clock
// of its own — so every rule is testable with a time.Time.
package notices

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// Digest modes: one list of times for weekdays and one for weekends, or a
// list per day.
const (
	ModeWeekdaysWeekends = "weekdays-weekends"
	ModePerDay           = "per-day"
)

// Severities the routing table knows. "recovered" governs the all-clears of
// faults that were themselves sent immediately; all-clears of digested faults
// always ride the digest.
var Severities = []string{"critical", "warning", "info", "recovered"}

// dayKeys are the per-day schedule keys, Monday first.
var dayKeys = []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}

type Policy struct {
	Immediate map[string]bool `json:"immediate"`
	Quiet     Quiet           `json:"quietHours"`
	Digest    Digest          `json:"digest"`
	Timezone  string          `json:"timezone"`
}

type Quiet struct {
	Enabled        bool   `json:"enabled"`
	From           string `json:"from"`
	To             string `json:"to"`
	CriticalDuring bool   `json:"criticalDuring"`
}

type Digest struct {
	Mode     string              `json:"mode"`
	Weekdays []string            `json:"weekdays"`
	Weekends []string            `json:"weekends"`
	Days     map[string][]string `json:"days"`
}

// Default is the policy a fresh install gets: only critical interrupts,
// quiet from half past ten to seven with critical still allowed through,
// two digests on weekdays and two later ones at the weekend.
func Default(timezone string) Policy {
	return Policy{
		Immediate: map[string]bool{"critical": true, "warning": false, "info": false, "recovered": true},
		Quiet:     Quiet{Enabled: true, From: "22:30", To: "07:00", CriticalDuring: true},
		Digest: Digest{
			Mode:     ModeWeekdaysWeekends,
			Weekdays: []string{"08:00", "18:30"},
			Weekends: []string{"10:00", "19:00"},
			Days:     map[string][]string{},
		},
		Timezone: timezone,
	}
}

// HostTimezone is the IANA zone the host runs in, or UTC when it cannot be
// told: time.Local knows the rules but not the name.
func HostTimezone() string {
	if tz := os.Getenv("TZ"); tz != "" {
		if _, err := time.LoadLocation(tz); err == nil {
			return tz
		}
	}
	if raw, err := os.ReadFile("/etc/timezone"); err == nil {
		if tz := strings.TrimSpace(string(raw)); tz != "" {
			if _, err := time.LoadLocation(tz); err == nil {
				return tz
			}
		}
	}
	if link, err := os.Readlink("/etc/localtime"); err == nil {
		if i := strings.Index(link, "zoneinfo/"); i >= 0 {
			tz := link[i+len("zoneinfo/"):]
			if _, err := time.LoadLocation(tz); err == nil {
				return tz
			}
		}
	}
	return "UTC"
}

// Normalized fills what an older or partial stored policy lacks and puts
// every time list in order without duplicates.
func (p Policy) Normalized() Policy {
	def := Default(p.Timezone)
	if p.Immediate == nil {
		p.Immediate = map[string]bool{}
	}
	for _, sev := range Severities {
		if _, ok := p.Immediate[sev]; !ok {
			p.Immediate[sev] = def.Immediate[sev]
		}
	}
	p.Immediate["critical"] = true // critical always interrupts; the toggle is not offered
	if p.Quiet.From == "" {
		p.Quiet.From = def.Quiet.From
	}
	if p.Quiet.To == "" {
		p.Quiet.To = def.Quiet.To
	}
	if p.Digest.Mode == "" {
		p.Digest.Mode = def.Digest.Mode
	}
	if p.Digest.Weekdays == nil {
		p.Digest.Weekdays = def.Digest.Weekdays
	}
	if p.Digest.Weekends == nil {
		p.Digest.Weekends = def.Digest.Weekends
	}
	if p.Digest.Days == nil {
		p.Digest.Days = map[string][]string{}
	}
	p.Digest.Weekdays = tidyTimes(p.Digest.Weekdays)
	p.Digest.Weekends = tidyTimes(p.Digest.Weekends)
	for _, d := range dayKeys {
		p.Digest.Days[d] = tidyTimes(p.Digest.Days[d])
	}
	if p.Timezone == "" {
		p.Timezone = HostTimezone()
	}
	return p
}

func tidyTimes(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, t := range in {
		t = strings.TrimSpace(t)
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// Validate refuses what the scheduler could not act on.
func (p Policy) Validate() error {
	if _, err := time.LoadLocation(p.Timezone); err != nil {
		return fmt.Errorf("unknown time zone %q", p.Timezone)
	}
	if p.Digest.Mode != ModeWeekdaysWeekends && p.Digest.Mode != ModePerDay {
		return errors.New("digest mode must be weekdays-weekends or per-day")
	}
	for _, t := range append(append([]string{}, p.Digest.Weekdays...), p.Digest.Weekends...) {
		if _, _, err := parseClock(t); err != nil {
			return err
		}
	}
	for day, times := range p.Digest.Days {
		if !contains(dayKeys, day) {
			return fmt.Errorf("unknown day %q", day)
		}
		for _, t := range times {
			if _, _, err := parseClock(t); err != nil {
				return err
			}
		}
	}
	if p.Quiet.Enabled {
		for _, t := range []string{p.Quiet.From, p.Quiet.To} {
			if _, _, err := parseClock(t); err != nil {
				return err
			}
		}
	}
	return nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// parseClock reads "HH:MM".
func parseClock(s string) (int, int, error) {
	var h, m int
	if n, err := fmt.Sscanf(strings.TrimSpace(s), "%d:%d", &h, &m); err != nil || n != 2 || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, fmt.Errorf("%q is not a time of day (HH:MM)", s)
	}
	return h, m, nil
}

// Location is the zone every clock time in the policy is read in.
func (p Policy) Location() *time.Location {
	if loc, err := time.LoadLocation(p.Timezone); err == nil {
		return loc
	}
	return time.UTC
}

// TimesFor lists the digest times for a weekday.
func (p Policy) TimesFor(day time.Weekday) []string {
	if p.Digest.Mode == ModePerDay {
		return p.Digest.Days[dayKeys[(int(day)+6)%7]]
	}
	if day == time.Saturday || day == time.Sunday {
		return p.Digest.Weekends
	}
	return p.Digest.Weekdays
}

// DigestDue reports whether a scheduled digest instant lies in (last, now].
// The caller keeps last as the previous check, so a digest is never sent
// twice and never missed across a slow tick.
func (p Policy) DigestDue(last, now time.Time) bool {
	loc := p.Location()
	if last.IsZero() || now.Sub(last) > 8*24*time.Hour {
		last = now.Add(-2 * time.Minute)
	}
	day := time.Date(last.In(loc).Year(), last.In(loc).Month(), last.In(loc).Day(), 0, 0, 0, 0, loc)
	end := now.In(loc)
	for !day.After(end) {
		for _, clock := range p.TimesFor(day.Weekday()) {
			h, m, err := parseClock(clock)
			if err != nil {
				continue
			}
			t := time.Date(day.Year(), day.Month(), day.Day(), h, m, 0, 0, loc)
			if t.After(last) && !t.After(now) {
				return true
			}
		}
		day = day.AddDate(0, 0, 1)
	}
	return false
}

// NextDigest is the first scheduled instant after now, if any within a week.
func (p Policy) NextDigest(now time.Time) (time.Time, bool) {
	loc := p.Location()
	cur := now.In(loc)
	day := time.Date(cur.Year(), cur.Month(), cur.Day(), 0, 0, 0, 0, loc)
	for i := 0; i < 8; i++ {
		for _, clock := range p.TimesFor(day.Weekday()) {
			h, m, err := parseClock(clock)
			if err != nil {
				continue
			}
			if t := time.Date(day.Year(), day.Month(), day.Day(), h, m, 0, 0, loc); t.After(now) {
				return t, true
			}
		}
		day = day.AddDate(0, 0, 1)
	}
	return time.Time{}, false
}

// InQuietHours reports whether now falls inside the quiet window, which may
// wrap past midnight.
func (p Policy) InQuietHours(now time.Time) bool {
	if !p.Quiet.Enabled {
		return false
	}
	fh, fm, err1 := parseClock(p.Quiet.From)
	th, tm, err2 := parseClock(p.Quiet.To)
	if err1 != nil || err2 != nil {
		return false
	}
	cur := now.In(p.Location())
	minute := cur.Hour()*60 + cur.Minute()
	from, to := fh*60+fm, th*60+tm
	if from == to {
		return false
	}
	if from < to {
		return minute >= from && minute < to
	}
	return minute >= from || minute < to
}

// Route is where a transition goes.
type Route int

const (
	RouteDigest    Route = iota // waits for the next digest
	RouteImmediate              // goes to the phone now
)

// RouteFor decides for a severity at a moment: immediate when the policy says
// so and the house is not quiet — critical during quiet hours only when
// allowed to be.
func (p Policy) RouteFor(severity string, now time.Time) Route {
	if !p.Immediate[severity] {
		return RouteDigest
	}
	if p.InQuietHours(now) {
		if severity == "critical" && p.Quiet.CriticalDuring {
			return RouteImmediate
		}
		return RouteDigest
	}
	return RouteImmediate
}
