package patchpilot

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

// nthDowRe matches the "nth weekday" extension in the day-of-week field, e.g.
// "6#1" (first Saturday) or "6#4" (fourth Saturday). Weekday is 0-6, Sun=0.
var nthDowRe = regexp.MustCompile(`^([0-6])#([1-4])$`)

// lastDowRe matches the "last weekday" extension, e.g. "6L" (last Saturday).
var lastDowRe = regexp.MustCompile(`^([0-6])L$`)

// nthLast is the sentinel occurrence value meaning "the last such weekday".
const nthLast = 5

// NextRun validates a cron expression and returns the next fire time after now.
//
// On top of standard 5-field cron it understands an nth-weekday extension in
// the day-of-week field — "6#1" (first Saturday of the month), "6#4" (fourth),
// "6L" (last Saturday). robfig/cron can't parse those, and the naive workaround
// (a day-of-month range like "1-7 * 6") fires with OR semantics — every day
// 1–7 *and* every Saturday — so we evaluate the extension ourselves.
func NextRun(expr string) (time.Time, error) {
	return nextRunAfter(expr, time.Now())
}

func nextRunAfter(expr string, after time.Time) (time.Time, error) {
	min, hour, dow, nth, ok, err := parseNthWeekday(expr)
	if err != nil {
		return time.Time{}, err
	}
	if ok {
		next := nextNthWeekday(after, hour, min, dow, nth)
		if next.IsZero() {
			return time.Time{}, fmt.Errorf("cron expression has no upcoming fire time")
		}
		return next, nil
	}
	sched, perr := cron.ParseStandard(expr)
	if perr != nil {
		return time.Time{}, fmt.Errorf("invalid cron expression: %w", perr)
	}
	return sched.Next(after), nil
}

// parseNthWeekday recognises the nth-weekday extension. It returns
// (minute, hour, weekday, nth, matched, error); nth is 1–4 for first–fourth or
// nthLast for "last". A non-extended expression returns matched=false, err=nil
// so the caller falls back to the standard parser.
func parseNthWeekday(expr string) (minute, hour, dow, nth int, matched bool, err error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return 0, 0, 0, 0, false, nil
	}
	switch dowField := fields[4]; {
	case nthDowRe.MatchString(dowField):
		m := nthDowRe.FindStringSubmatch(dowField)
		dow, _ = strconv.Atoi(m[1])
		nth, _ = strconv.Atoi(m[2])
	case lastDowRe.MatchString(dowField):
		m := lastDowRe.FindStringSubmatch(dowField)
		dow, _ = strconv.Atoi(m[1])
		nth = nthLast
	default:
		return 0, 0, 0, 0, false, nil
	}
	// The remaining fields must be simple for our evaluator: numeric minute and
	// hour, and no day-of-month / month restriction (those would conflict with
	// selecting by weekday-of-month).
	minute, e1 := strconv.Atoi(fields[0])
	hour, e2 := strconv.Atoi(fields[1])
	if e1 != nil || e2 != nil || fields[2] != "*" || fields[3] != "*" {
		return 0, 0, 0, 0, false, fmt.Errorf("nth-weekday schedules need a fixed minute and hour and no day-of-month or month restriction")
	}
	if minute < 0 || minute > 59 || hour < 0 || hour > 23 {
		return 0, 0, 0, 0, false, fmt.Errorf("minute or hour out of range")
	}
	return minute, hour, dow, nth, true, nil
}

// nextNthWeekday returns the first datetime strictly after `after` at hour:min
// on the nth (or last) occurrence of weekday dow. It walks month by month
// because a "fourth" occurrence can be absent from a short month; the loop is
// bounded generously (a valid occurrence always lands within ~2 months).
func nextNthWeekday(after time.Time, hour, minute, dow, nth int) time.Time {
	loc := after.Location()
	y, m := after.Year(), after.Month()
	for i := 0; i < 24; i++ {
		if day := nthWeekdayOfMonth(y, m, loc, dow, nth); day > 0 {
			cand := time.Date(y, m, day, hour, minute, 0, 0, loc)
			if cand.After(after) {
				return cand
			}
		}
		if m++; m > time.December {
			m = time.January
			y++
		}
	}
	return time.Time{}
}

// nthWeekdayOfMonth returns the day-of-month for the nth (1–4, or nthLast=last)
// occurrence of weekday dow in the given month, or 0 if it doesn't exist.
func nthWeekdayOfMonth(year int, month time.Month, loc *time.Location, dow, nth int) int {
	daysIn := time.Date(year, month, 1, 0, 0, 0, 0, loc).AddDate(0, 1, -1).Day()
	if nth == nthLast {
		lastWd := int(time.Date(year, month, daysIn, 0, 0, 0, 0, loc).Weekday())
		return daysIn - (lastWd-dow+7)%7
	}
	firstWd := int(time.Date(year, month, 1, 0, 0, 0, 0, loc).Weekday())
	day := 1 + (dow-firstWd+7)%7 + (nth-1)*7
	if day > daysIn {
		return 0
	}
	return day
}
