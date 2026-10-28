package calendar

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/teambition/rrule-go"

	"homehub/internal/store"
)

// Instance is one concrete occurrence shown on the calendar: either a
// non-recurring event, one expansion of a recurring master, or an override.
type Instance struct {
	EventID    uuid.UUID  `json:"eventId"`
	CalendarID uuid.UUID  `json:"calendarId"`
	UID        string     `json:"uid"`
	Title      string     `json:"title"`
	Location   string     `json:"location,omitempty"`
	Description string    `json:"description,omitempty"`
	URL        string     `json:"url,omitempty"`
	Start      time.Time  `json:"start"`
	End        time.Time  `json:"end"`
	AllDay     bool       `json:"allDay"`
	Recurring  bool       `json:"recurring"`
	// OccurrenceStart identifies the slot in the master's series this
	// instance fills (equal to Start unless an override moved it).
	OccurrenceStart time.Time `json:"occurrenceStart"`
}

// maxInstancesPerEvent caps runaway rules.
const maxInstancesPerEvent = 750

// maxRuleSteps bounds how far one rule is walked, counting the occurrences
// before the window too. Capping only the result is not enough: Between
// materialises every occurrence first, so FREQ=SECONDLY over a year-long
// window is tens of millions of timestamps per request — and every reminder
// tick and feed poll expands again.
const maxRuleSteps = 100_000

// Expand turns event rows into concrete instances within [from, to).
// Overrides replace their master's occurrence; EXDATEs remove theirs.
func Expand(rows []store.CalendarEvent, from, to time.Time) []Instance {
	// Group overrides by (calendar, uid) → occurrence start.
	type key struct {
		cal uuid.UUID
		uid string
	}
	overrides := map[key]map[int64]store.CalendarEvent{}
	for _, r := range rows {
		if r.RecurrenceID == nil {
			continue
		}
		k := key{r.CalendarID, r.UID}
		if overrides[k] == nil {
			overrides[k] = map[int64]store.CalendarEvent{}
		}
		overrides[k][r.RecurrenceID.UTC().Unix()] = r
	}

	var out []Instance
	push := func(row store.CalendarEvent, start, end time.Time, occurrence time.Time, recurring bool) {
		if !start.Before(to) || !end.After(from) {
			return
		}
		out = append(out, Instance{
			EventID:     row.ID,
			CalendarID:  row.CalendarID,
			UID:         row.UID,
			Title:       row.Title,
			Location:    row.Location,
			Description: row.Description,
			URL:         row.URL,
			Start:       start,
			End:         end,
			AllDay:      row.AllDay,
			Recurring:   recurring,
			OccurrenceStart: occurrence,
		})
	}

	for _, r := range rows {
		if r.RecurrenceID != nil {
			continue // handled through the master (or as orphan below)
		}
		k := key{r.CalendarID, r.UID}
		ovs := overrides[k]

		if r.RRule == "" {
			push(r, r.StartsAt, r.EndsAt, r.StartsAt, false)
			continue
		}

		opt, err := rrule.StrToROption(r.RRule)
		if err != nil {
			push(r, r.StartsAt, r.EndsAt, r.StartsAt, false)
			continue
		}
		opt.Dtstart = r.StartsAt.UTC()
		rule, err := rrule.NewRRule(*opt)
		if err != nil {
			push(r, r.StartsAt, r.EndsAt, r.StartsAt, false)
			continue
		}
		duration := r.EndsAt.Sub(r.StartsAt)
		ex := map[int64]bool{}
		for _, e := range r.Exdates {
			ex[e.UTC().Unix()] = true
		}
		// Expand a window slightly widened so events spanning the boundary
		// still appear.
		starts := boundedBetween(rule, from.Add(-duration).Add(-time.Second), to)
		for _, s := range starts {
			slot := s.UTC()
			if ex[slot.Unix()] {
				continue
			}
			if ov, has := ovs[slot.Unix()]; has {
				push(ov, ov.StartsAt, ov.EndsAt, slot, true)
				delete(ovs, slot.Unix())
				continue
			}
			push(r, slot, slot.Add(duration), slot, true)
		}
	}

	// Orphan overrides (master missing or slot outside expansion) still show.
	for k, ovs := range overrides {
		_ = k
		for slot, ov := range ovs {
			push(ov, ov.StartsAt, ov.EndsAt, time.Unix(slot, 0).UTC(), true)
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out
}

// boundedBetween is rule.Between with both caps applied while walking, so a
// dense rule costs at most maxRuleSteps iterations however wide the window.
func boundedBetween(rule *rrule.RRule, after, before time.Time) []time.Time {
	next := rule.Iterator()
	var out []time.Time
	for steps := 0; steps < maxRuleSteps; steps++ {
		t, ok := next()
		if !ok || t.After(before) {
			break
		}
		if t.Before(after) {
			continue
		}
		out = append(out, t)
		if len(out) == maxInstancesPerEvent {
			break
		}
	}
	return out
}

// ValidateRRule refuses rules no household calendar needs and that only
// serve to burn CPU: anything recurring more often than hourly, directly or
// through BYMINUTE/BYSECOND expansion.
func ValidateRRule(raw string) error {
	if raw == "" {
		return nil
	}
	opt, err := rrule.StrToROption(strings.TrimPrefix(raw, "RRULE:"))
	if err != nil {
		return fmt.Errorf("the repeat rule is not valid: %w", err)
	}
	if opt.Freq == rrule.SECONDLY || opt.Freq == rrule.MINUTELY {
		return errors.New("events can repeat at most hourly")
	}
	// BY* parts multiply: DAILY with 24 hours × 60 minutes is minutely again.
	perDay := max(1, len(opt.Byhour)) * max(1, len(opt.Byminute)) * max(1, len(opt.Bysecond))
	if opt.Freq == rrule.HOURLY {
		perDay = 24 * max(1, len(opt.Byminute)) * max(1, len(opt.Bysecond))
	}
	if perDay > 24 {
		return errors.New("events can repeat at most hourly")
	}
	return nil
}
