package calendar

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/emersion/go-ical"
	"github.com/google/uuid"

	"homehub/internal/store"
)

// ParseICS turns an iCalendar document into event rows for one calendar:
// masters (with RRULE/EXDATE) and override instances (RECURRENCE-ID).
func ParseICS(calendarID uuid.UUID, data []byte, href, etag string) ([]store.CalendarEvent, error) {
	cal, err := ical.NewDecoder(bytes.NewReader(data)).Decode()
	if err != nil {
		return nil, fmt.Errorf("parse ics: %w", err)
	}
	return eventsFromCal(calendarID, cal, href, etag), nil
}

func eventsFromCal(calendarID uuid.UUID, cal *ical.Calendar, href, etag string) []store.CalendarEvent {
	var out []store.CalendarEvent
	for _, ev := range cal.Events() {
		row, ok := eventFromComponent(calendarID, ev, href, etag)
		if ok {
			out = append(out, row)
		}
	}
	return out
}

func eventFromComponent(calendarID uuid.UUID, ev ical.Event, href, etag string) (store.CalendarEvent, bool) {
	uid, _ := ev.Props.Text(ical.PropUID)
	if uid == "" {
		return store.CalendarEvent{}, false
	}
	start, err := ev.DateTimeStart(time.UTC)
	if err != nil {
		return store.CalendarEvent{}, false
	}
	allDay := false
	if p := ev.Props.Get(ical.PropDateTimeStart); p != nil && p.ValueType() == ical.ValueDate {
		allDay = true
	}
	end, err := ev.DateTimeEnd(time.UTC)
	if err != nil || end.IsZero() || !end.After(start) {
		if allDay {
			end = start.Add(24 * time.Hour)
		} else {
			end = start.Add(time.Hour)
		}
	}

	title, _ := ev.Props.Text(ical.PropSummary)
	location, _ := ev.Props.Text(ical.PropLocation)
	description, _ := ev.Props.Text(ical.PropDescription)
	urlStr := ""
	if p := ev.Props.Get("URL"); p != nil {
		urlStr = p.Value
	}

	rrule := ""
	if p := ev.Props.Get(ical.PropRecurrenceRule); p != nil {
		rrule = p.Value
	}

	var exdates []time.Time
	for _, p := range ev.Props[ical.PropExceptionDates] {
		for _, v := range strings.Split(p.Value, ",") {
			if t, terr := parseICalTime(strings.TrimSpace(v)); terr == nil {
				exdates = append(exdates, t)
			}
		}
	}

	var recurrenceID *time.Time
	if p := ev.Props.Get(ical.PropRecurrenceID); p != nil {
		if t, terr := propDateTime(p, start); terr == nil {
			recurrenceID = &t
		}
	}

	return store.CalendarEvent{
		CalendarID:   calendarID,
		UID:          uid,
		RecurrenceID: recurrenceID,
		Etag:         etag,
		Href:         href,
		Title:        title,
		Location:     location,
		Description:  description,
		URL:          urlStr,
		StartsAt:     start.UTC(),
		EndsAt:       end.UTC(),
		AllDay:       allDay,
		RRule:        strings.TrimPrefix(rrule, "RRULE:"),
		Exdates:      exdates,
	}, true
}

// propDateTime reads a date or date-time prop's (first) value.
func propDateTime(p *ical.Prop, _ time.Time) (time.Time, error) {
	v := p.Value
	if i := strings.IndexByte(v, ','); i >= 0 {
		v = v[:i]
	}
	return parseICalTime(v)
}

func parseICalTime(v string) (time.Time, error) {
	layouts := []string{"20060102T150405Z", "20060102T150405", "20060102"}
	for _, l := range layouts {
		if t, err := time.Parse(l, v); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unparseable date %q", v)
}

// BuildICS serializes one event (master + overrides) into an iCalendar
// document for CalDAV round-trips.
func BuildICS(master store.CalendarEvent, overrides []store.CalendarEvent) ([]byte, error) {
	cal := ical.NewCalendar()
	cal.Props.SetText(ical.PropProductID, "-//HomeHub//Calendar//EN")
	cal.Props.SetText(ical.PropVersion, "2.0")

	appendEvent := func(row store.CalendarEvent) {
		ev := ical.NewEvent()
		ev.Props.SetText(ical.PropUID, row.UID)
		ev.Props.SetDateTime(ical.PropDateTimeStamp, time.Now().UTC())
		if row.AllDay {
			startProp := ical.NewProp(ical.PropDateTimeStart)
			startProp.SetValueType(ical.ValueDate)
			startProp.Value = row.StartsAt.UTC().Format("20060102")
			ev.Props.Set(startProp)
			endProp := ical.NewProp(ical.PropDateTimeEnd)
			endProp.SetValueType(ical.ValueDate)
			endProp.Value = row.EndsAt.UTC().Format("20060102")
			ev.Props.Set(endProp)
		} else {
			ev.Props.SetDateTime(ical.PropDateTimeStart, row.StartsAt.UTC())
			ev.Props.SetDateTime(ical.PropDateTimeEnd, row.EndsAt.UTC())
		}
		ev.Props.SetText(ical.PropSummary, row.Title)
		if row.Location != "" {
			ev.Props.SetText(ical.PropLocation, row.Location)
		}
		if row.Description != "" {
			ev.Props.SetText(ical.PropDescription, row.Description)
		}
		if row.URL != "" {
			ev.Props.SetText("URL", row.URL)
		}
		if row.RRule != "" {
			p := ical.NewProp(ical.PropRecurrenceRule)
			p.Value = row.RRule
			ev.Props.Set(p)
		}
		for _, ex := range row.Exdates {
			p := ical.NewProp(ical.PropExceptionDates)
			p.Value = ex.UTC().Format("20060102T150405Z")
			ev.Props.Add(p)
		}
		if row.RecurrenceID != nil {
			p := ical.NewProp(ical.PropRecurrenceID)
			p.Value = row.RecurrenceID.UTC().Format("20060102T150405Z")
			ev.Props.Set(p)
		}
		cal.Children = append(cal.Children, ev.Component)
	}

	appendEvent(master)
	for _, o := range overrides {
		appendEvent(o)
	}

	var buf bytes.Buffer
	if err := ical.NewEncoder(&buf).Encode(cal); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// parseForPut re-decodes serialized ICS into the library's calendar type
// (PutCalendarObject wants *ical.Calendar).
func parseForPut(data []byte) (*ical.Calendar, error) {
	return ical.NewDecoder(bytes.NewReader(data)).Decode()
}
