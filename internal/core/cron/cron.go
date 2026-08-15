// Package cron parses a crontab schedule and answers when it next fires.
//
// # Why this is written here rather than depended on
//
// This is a five-field grammar that has not changed since the 1970s and a next-fire search that is
// a few dozen lines. The tree's dependency list is short on purpose, and a scheduler library brings
// a job runner, a logger and a retry policy along with the parser — three things this tree already
// has opinions about. What is wanted is the grammar.
//
// # What it deliberately does not do
//
// It does not run anything, hold a clock, or remember that it fired. Next is a pure function of a
// schedule and an instant, which is what makes the runner in internal/app testable against a fake
// clock instead of against real time.
//
// # Local time, and what that costs
//
// Schedules are read in the location of the instant handed to Next, because "0 3 * * *" means three
// in the morning where the person is, not 03:00 UTC.
//
// Daylight saving is therefore visible, in both directions, and the rule that produces it is the
// only one here: Next moves forward in absolute time and returns the first instant whose WALL clock
// matches. On the spring day an hour disappears, a schedule inside that hour does not fire at all —
// no instant that day carries that wall time. On the autumn day an hour repeats, a schedule inside
// it fires TWICE, because 01:30 happens at two distinct instants and both match.
//
// Neither is papered over. The alternative — a rule that suppresses the second pass — needs state
// about what already fired, which is the runner's job and not the calendar's, and it buys a
// once-a-year edge at the cost of a schedule that cannot be reasoned about from the spec alone.
// Both behaviours are pinned by tests so they stay deliberate.
package cron

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// searchYears bounds the next-fire search. A schedule like "0 0 30 2 *" (the 30th of February)
// parses fine and never fires; without a bound Next would walk the calendar forever looking for it.
// Five years is far past any real schedule's period and is reached in a few hundred iterations.
const searchYears = 5

// Schedule is a parsed crontab line. The five fields are bitsets over their own ranges.
type Schedule struct {
	spec  string
	min   uint64 // 0-59
	hour  uint64 // 0-23
	dom   uint64 // 1-31
	month uint64 // 1-12
	dow   uint64 // 0-6, Sunday = 0
	// domRestricted and dowRestricted record whether the day-of-month and day-of-week fields were
	// written as a bare "*". They are not derivable from the bitsets — "*" and "0-6" set the same
	// bits and mean different things to the OR rule in dayMatch.
	domRestricted bool
	dowRestricted bool
}

// String returns the schedule as it was written, for display.
func (s Schedule) String() string { return s.spec }

// aliases are the @-shorthands, expanded to the five-field form before parsing so there is one
// grammar and not two. @reboot is absent on purpose: it is not a time, and a daemon that honoured
// it would fire every job every time it restarted.
var aliases = map[string]string{
	"@yearly":   "0 0 1 1 *",
	"@annually": "0 0 1 1 *",
	"@monthly":  "0 0 1 * *",
	"@weekly":   "0 0 * * 0",
	"@daily":    "0 0 * * *",
	"@midnight": "0 0 * * *",
	"@hourly":   "0 * * * *",
}

var monthNames = map[string]int{
	"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6,
	"jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12,
}

var dowNames = map[string]int{
	"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6,
}

type fieldDef struct {
	label    string
	min, max int
	names    map[string]int
}

var fields = []fieldDef{
	{"minute", 0, 59, nil},
	{"hour", 0, 23, nil},
	{"day of month", 1, 31, nil},
	{"month", 1, 12, monthNames},
	// Seven is accepted for Sunday, as crontab does, and folded onto zero at parse time so the
	// bitset has one spelling of the day.
	{"day of week", 0, 7, dowNames},
}

// Parse reads a crontab schedule: five space-separated fields, or one of the @-aliases.
//
// Each field is a comma-separated list of terms, where a term is "*", a number, "a-b", or either of
// those followed by "/n". Month and day-of-week also accept three-letter names (jan, mon).
func Parse(spec string) (Schedule, error) {
	trimmed := strings.TrimSpace(spec)
	if trimmed == "" {
		return Schedule{}, fmt.Errorf("a schedule cannot be empty")
	}
	line := trimmed
	if strings.HasPrefix(line, "@") {
		expanded, ok := aliases[strings.ToLower(line)]
		if !ok {
			return Schedule{}, fmt.Errorf("unknown shorthand %q (known: @yearly @annually @monthly @weekly @daily @midnight @hourly)", line)
		}
		line = expanded
	}
	parts := strings.Fields(line)
	if len(parts) != 5 {
		return Schedule{}, fmt.Errorf("a schedule has 5 fields (minute hour day-of-month month day-of-week), got %d in %q", len(parts), trimmed)
	}
	var bits [5]uint64
	for i, p := range parts {
		b, err := parseField(p, fields[i])
		if err != nil {
			return Schedule{}, fmt.Errorf("%s field %q: %w", fields[i].label, p, err)
		}
		bits[i] = b
	}
	// Fold day-of-week 7 onto 0 so Sunday has one bit. Done after parsing so "5-7" still means
	// Friday through Sunday rather than an empty range.
	if bits[4]&(1<<7) != 0 {
		bits[4] &^= 1 << 7
		bits[4] |= 1 << 0
	}
	return Schedule{
		spec:          trimmed,
		min:           bits[0],
		hour:          bits[1],
		dom:           bits[2],
		month:         bits[3],
		dow:           bits[4],
		domRestricted: parts[2] != "*",
		dowRestricted: parts[4] != "*",
	}, nil
}

// parseField turns one comma-separated field into a bitset.
func parseField(text string, def fieldDef) (uint64, error) {
	var bits uint64
	for _, term := range strings.Split(text, ",") {
		term = strings.TrimSpace(term)
		if term == "" {
			return 0, fmt.Errorf("empty term")
		}
		b, err := parseTerm(term, def)
		if err != nil {
			return 0, err
		}
		bits |= b
	}
	if bits == 0 {
		return 0, fmt.Errorf("matches nothing")
	}
	return bits, nil
}

// parseTerm handles one term: "*", "n", "a-b", and any of those with a "/step" suffix.
func parseTerm(term string, def fieldDef) (uint64, error) {
	body, stepText, hasStep := strings.Cut(term, "/")
	step := 1
	if hasStep {
		n, err := strconv.Atoi(strings.TrimSpace(stepText))
		if err != nil || n <= 0 {
			return 0, fmt.Errorf("step must be a positive number, got %q", stepText)
		}
		step = n
	}
	body = strings.TrimSpace(body)

	var lo, hi int
	switch {
	case body == "*":
		lo, hi = def.min, def.max
	case strings.Contains(body, "-"):
		a, b, _ := strings.Cut(body, "-")
		var err error
		if lo, err = parseValue(a, def); err != nil {
			return 0, err
		}
		if hi, err = parseValue(b, def); err != nil {
			return 0, err
		}
		if lo > hi {
			// Refused rather than wrapped around. A wrapping range is not in the grammar, and
			// guessing which of the two the writer meant is how "5-1" silently becomes every day.
			return 0, fmt.Errorf("range %q runs backwards", body)
		}
	default:
		v, err := parseValue(body, def)
		if err != nil {
			return 0, err
		}
		lo = v
		// A bare number with a step means "from here to the end of the field", which is how
		// crontab reads "5/10" in the minute field.
		if hasStep {
			hi = def.max
		} else {
			hi = v
		}
	}
	var bits uint64
	for v := lo; v <= hi; v += step {
		bits |= 1 << uint(v)
	}
	return bits, nil
}

// parseValue reads one number, or a three-letter name where the field allows them.
func parseValue(text string, def fieldDef) (int, error) {
	text = strings.TrimSpace(text)
	if def.names != nil {
		if v, ok := def.names[strings.ToLower(text)]; ok {
			return v, nil
		}
	}
	v, err := strconv.Atoi(text)
	if err != nil {
		return 0, fmt.Errorf("%q is not a number", text)
	}
	if v < def.min || v > def.max {
		return 0, fmt.Errorf("%d is outside %d-%d", v, def.min, def.max)
	}
	return v, nil
}

func bit(set uint64, v int) bool { return set&(1<<uint(v)) != 0 }

// dayMatch applies crontab's day rule, which is an OR and not an AND.
//
// "0 0 13 * 5" is the 13th of the month AND every Friday, not only Friday the 13th. The rule is
// that when both day fields are restricted either one matching is enough; when only one is
// restricted that one decides. It surprises everybody once, which is why it is a named method with
// this comment on it rather than a condition inside the search loop.
func (s Schedule) dayMatch(t time.Time) bool {
	dom := bit(s.dom, t.Day())
	dow := bit(s.dow, int(t.Weekday()))
	switch {
	case s.domRestricted && s.dowRestricted:
		return dom || dow
	case s.domRestricted:
		return dom
	case s.dowRestricted:
		return dow
	default:
		return true
	}
}

// Next returns the first instant strictly after t at which the schedule fires, in t's location.
//
// The zero time means the schedule never fires again within searchYears — a real answer for a real
// schedule ("the 30th of February"), and one the caller has to handle rather than wait on.
func (s Schedule) Next(t time.Time) time.Time {
	fire, _ := s.next(t)
	return fire
}

// next is Next, and also says how many times round the search loop it went.
//
// The step count exists for one test. The coarse-to-fine skipping below is a performance property
// with no effect on the answer, so the only honest way to hold onto it is to measure the work — and
// a wall-clock assertion could not: crawling five years a minute at a time still finishes inside
// any timeout loose enough not to flake, so the timing version of this test passed with the
// skipping removed. Counting steps is deterministic and says the thing it means.
func (s Schedule) next(t time.Time) (time.Time, int) {
	// The zero Schedule never fires. Tested on spec rather than on an empty bitset: Parse always
	// records the text it read, and a bitset can only be empty by never having been filled, so
	// asking the field that says "this was parsed" keeps one fact in one place.
	if s.spec == "" {
		return time.Time{}, 0
	}
	loc := t.Location()
	// Start at the next whole minute. Strictly after, so a job asked for its next fire during the
	// very minute it just fired gets the following one instead of firing twice.
	next := time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, loc).Add(time.Minute)
	limit := next.AddDate(searchYears, 0, 0)

	// Coarse-to-fine: skip a whole month, day or hour when its field does not match, so the search
	// costs a few hundred steps rather than the couple of million minutes in five years.
	steps := 0
	for next.Before(limit) {
		steps++
		if !bit(s.month, int(next.Month())) {
			next = time.Date(next.Year(), next.Month(), 1, 0, 0, 0, 0, loc).AddDate(0, 1, 0)
			continue
		}
		if !s.dayMatch(next) {
			next = time.Date(next.Year(), next.Month(), next.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, 1)
			continue
		}
		if !bit(s.hour, next.Hour()) {
			cand := time.Date(next.Year(), next.Month(), next.Day(), next.Hour(), 0, 0, 0, loc).Add(time.Hour)
			// Strict progress. On a fall-back DST day the wall-hour repeats (01:00 occurs twice), and
			// time.Date resolves the ambiguous hour to the EARLIER instant — so cand could land on the
			// same absolute time as next and the search would spin here forever for any ordinary
			// schedule ("0 3 * * *") evaluated during those early hours. If the coarse skip did not
			// move forward in absolute time, step by an hour from next itself, which always does.
			if !cand.After(next) {
				cand = next.Add(time.Hour)
			}
			next = cand
			continue
		}
		if !bit(s.min, next.Minute()) {
			next = next.Add(time.Minute)
			continue
		}
		return next, steps
	}
	return time.Time{}, steps
}
