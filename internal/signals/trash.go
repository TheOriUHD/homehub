package signals

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"homehub/internal/calendar"
)

// trashCollector raises staged reminders for waste-collection (Müllabfuhr)
// pickups from trash-kind calendars: one signal per pickup per configured
// lead stage (e.g. 3 days and 24 hours ahead). Once the pickup passes, the
// signal turns green ("picked up") and waits for a casual dismiss.
type trashCollector struct{ Deps }

func (c *trashCollector) Name() string            { return "trash" }
func (c *trashCollector) Interval() time.Duration { return 10 * time.Minute }

func humanLead(hours int) string {
	switch {
	case hours >= 48:
		return fmt.Sprintf("in %d days", hours/24)
	case hours >= 24:
		return "tomorrow"
	default:
		return fmt.Sprintf("in %d hours", hours)
	}
}

func (c *trashCollector) Collect(ctx context.Context) ([]Observation, error) {
	cals, err := c.Store.ListCalendars(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	var out []Observation
	for _, cal := range cals {
		if cal.Kind != calendar.KindTrash {
			continue
		}
		settings := calendar.ParseTrashSettings(cal.Settings)
		maxLead := 0
		for _, h := range settings.LeadHours {
			if h > maxLead {
				maxLead = h
			}
		}
		events, eerr := c.Store.ListCalendarEventsInRange(ctx,
			[]uuid.UUID{cal.ID}, now.Add(-24*time.Hour), now.Add(time.Duration(maxLead+26)*time.Hour))
		if eerr != nil {
			continue
		}
		instances := calendar.Expand(events, now.Add(-24*time.Hour), now.Add(time.Duration(maxLead+26)*time.Hour))
		for _, inst := range instances {
			pickupOver := inst.End
			if inst.AllDay {
				pickupOver = inst.Start.Add(24 * time.Hour)
			}
			if now.After(pickupOver) {
				continue // fully in the past — signal auto-resolves
			}
			// One signal per pickup that escalates through its stages: the
			// title updates from "in 3 days" to "tomorrow" instead of a
			// second row appearing.
			due := false
			stage := 0
			for _, lead := range settings.LeadHours {
				if !now.Before(inst.Start.Add(-time.Duration(lead) * time.Hour)) {
					due = true
					if stage == 0 || lead < stage {
						stage = lead
					}
				}
			}
			if !due {
				continue
			}
			day := inst.Start.Format("Mon, Jan 2")
			title := fmt.Sprintf("%s — pickup %s", inst.Title, humanLead(int(time.Until(inst.Start).Hours())+1))
			if time.Until(inst.Start) <= 0 {
				title = fmt.Sprintf("%s — pickup is today", inst.Title)
			}
			calID := cal.ID
			out = append(out, Observation{
				Key:      fmt.Sprintf("trash:%s:%s:%d", cal.ID, inst.UID, inst.OccurrenceStart.Unix()),
				Severity: SevInfo,
				Title:    title,
				Body:     fmt.Sprintf("%s on %s. Put the bin out.", inst.Title, day),
				Detail: map[string]any{
					"type": inst.Title, "pickup": inst.Start, "calendar": cal.Name, "stageHours": stage,
				},
				SourceType: "calendar", SourceID: &calID, SourceName: cal.Name,
				AutoResolve:  true,
				ResolveTitle: fmt.Sprintf("%s was picked up", inst.Title),
				ResolveBody:  fmt.Sprintf("The %s pickup on %s is done.", inst.Title, day),
			})
		}
	}
	return out, nil
}
