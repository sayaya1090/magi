package cron

import (
	"testing"
	"time"
)

// at builds a UTC instant, for the tests that do not care about zones.
func at(y int, mo time.Month, d, h, mi int) time.Time {
	return time.Date(y, mo, d, h, mi, 0, 0, time.UTC)
}

func mustParse(t *testing.T, spec string) Schedule {
	t.Helper()
	s, err := Parse(spec)
	if err != nil {
		t.Fatalf("Parse(%q): %v", spec, err)
	}
	return s
}

func TestEveryFormInTheGrammarPicksTheRightMinutes(t *testing.T) {
	// One table, because the grammar is small and the interesting part is that each form lands on
	// the minutes it claims — not that it parses without error.
	cases := []struct {
		spec string
		from time.Time
		want time.Time
	}{
		{"* * * * *", at(2026, 8, 10, 9, 15), at(2026, 8, 10, 9, 16)},
		{"0 * * * *", at(2026, 8, 10, 9, 15), at(2026, 8, 10, 10, 0)},
		{"*/15 * * * *", at(2026, 8, 10, 9, 15), at(2026, 8, 10, 9, 30)},
		{"*/15 * * * *", at(2026, 8, 10, 9, 46), at(2026, 8, 10, 10, 0)},
		// A bare number with a step runs to the end of the field: 5/10 is 5,15,25,35,45,55.
		{"5/10 * * * *", at(2026, 8, 10, 9, 6), at(2026, 8, 10, 9, 15)},
		{"5/10 * * * *", at(2026, 8, 10, 9, 56), at(2026, 8, 10, 10, 5)},
		{"0 9-17 * * *", at(2026, 8, 10, 3, 0), at(2026, 8, 10, 9, 0)},
		{"0 9-17 * * *", at(2026, 8, 10, 17, 0), at(2026, 8, 11, 9, 0)},
		{"30 1-5/2 * * *", at(2026, 8, 10, 0, 0), at(2026, 8, 10, 1, 30)},
		{"30 1-5/2 * * *", at(2026, 8, 10, 1, 30), at(2026, 8, 10, 3, 30)},
		{"0 0 1,15 * *", at(2026, 8, 2, 0, 0), at(2026, 8, 15, 0, 0)},
		{"0 0 1,15 * *", at(2026, 8, 16, 0, 0), at(2026, 9, 1, 0, 0)},
		// Names, in the two fields that take them.
		{"0 0 1 jan *", at(2026, 8, 10, 0, 0), at(2027, 1, 1, 0, 0)},
		{"0 0 * * mon", at(2026, 8, 10, 1, 0), at(2026, 8, 17, 0, 0)}, // 2026-08-10 is a Monday
		// Sunday is both 0 and 7.
		{"0 0 * * 7", at(2026, 8, 10, 0, 0), at(2026, 8, 16, 0, 0)},
		{"0 0 * * 0", at(2026, 8, 10, 0, 0), at(2026, 8, 16, 0, 0)},
	}
	for _, c := range cases {
		got := mustParse(t, c.spec).Next(c.from)
		if !got.Equal(c.want) {
			t.Errorf("%q from %s: got %s, want %s", c.spec, c.from.Format(time.RFC3339),
				got.Format(time.RFC3339), c.want.Format(time.RFC3339))
		}
	}
}

func TestTheShorthandsMeanWhatTheirLongFormMeans(t *testing.T) {
	// Written as an equivalence rather than as expected instants: the point of an alias is that it
	// is the other spelling, and a table of times would let the two drift apart.
	pairs := [][2]string{
		{"@hourly", "0 * * * *"},
		{"@daily", "0 0 * * *"},
		{"@midnight", "0 0 * * *"},
		{"@weekly", "0 0 * * 0"},
		{"@monthly", "0 0 1 * *"},
		{"@yearly", "0 0 1 1 *"},
		{"@annually", "0 0 1 1 *"},
	}
	from := at(2026, 8, 10, 9, 15)
	for _, p := range pairs {
		a, b := mustParse(t, p[0]), mustParse(t, p[1])
		if !a.Next(from).Equal(b.Next(from)) {
			t.Errorf("%s fires at %s but %s fires at %s", p[0], a.Next(from), p[1], b.Next(from))
		}
	}
	// @reboot is not a time and is refused rather than quietly treated as one.
	if _, err := Parse("@reboot"); err == nil {
		t.Error("@reboot parsed; it is not a schedule and a daemon honouring it would fire every restart")
	}
}

// TestTheTwoDayFieldsAreOredNotAnded pins crontab's most surprising rule. It is the one thing here
// that a reader is likely to "fix" into an AND, so it is tested from both sides.
func TestTheTwoDayFieldsAreOredNotAnded(t *testing.T) {
	// The 13th of any month, and every Friday. August 2026: the 13th is a Thursday, and the
	// Fridays are the 7th, 14th, 21st, 28th.
	s := mustParse(t, "0 0 13 * fri")
	var fired []int
	// Bounded, and it checks that each answer moved. Walking Next in a loop is the natural way to
	// ask "which days in August", but it assumes Next advances — and when that assumption is what
	// broke, an unbounded loop hangs the test run instead of reporting the fault. A probe that can
	// hang is not a probe.
	cur := at(2026, 8, 1, 0, 0)
	for range 40 {
		n := s.Next(cur)
		if n.IsZero() {
			t.Fatalf("Next returned the zero time after %v", fired)
		}
		if !n.After(cur) {
			t.Fatalf("Next(%s) = %s did not move forward", cur, n)
		}
		if n.Month() != time.August || n.Year() != 2026 {
			break
		}
		fired = append(fired, n.Day())
		cur = n
	}
	want := []int{7, 13, 14, 21, 28}
	if len(fired) != len(want) {
		t.Fatalf("fired on %v, want %v", fired, want)
	}
	for i := range want {
		if fired[i] != want[i] {
			t.Fatalf("fired on %v, want %v", fired, want)
		}
	}

	// With only one of the two restricted, that one decides — no OR to fall back on.
	onlyDOM := mustParse(t, "0 0 13 * *")
	if got := onlyDOM.Next(at(2026, 8, 1, 0, 0)); got.Day() != 13 {
		t.Errorf("day-of-month alone: fired on the %d, want the 13th", got.Day())
	}
	onlyDOW := mustParse(t, "0 0 * * fri")
	if got := onlyDOW.Next(at(2026, 8, 1, 0, 0)); got.Day() != 7 {
		t.Errorf("day-of-week alone: fired on the %d, want the 7th (first Friday)", got.Day())
	}
}

func TestItCrossesMonthAndYearBoundaries(t *testing.T) {
	// Last minute of a year to the first of the next.
	if got, want := mustParse(t, "* * * * *").Next(at(2026, 12, 31, 23, 59)), at(2027, 1, 1, 0, 0); !got.Equal(want) {
		t.Errorf("year rollover: got %s, want %s", got, want)
	}
	// The 31st only exists in some months: from March, the next is May.
	if got, want := mustParse(t, "0 0 31 * *").Next(at(2026, 3, 31, 0, 0)), at(2026, 5, 31, 0, 0); !got.Equal(want) {
		t.Errorf("short months: got %s, want %s", got, want)
	}
}

func TestTheTwentyNinthOfFebruaryWaitsForALeapYear(t *testing.T) {
	// 2026 is not a leap year; 2028 is.
	got := mustParse(t, "0 0 29 2 *").Next(at(2026, 3, 1, 0, 0))
	if want := at(2028, 2, 29, 0, 0); !got.Equal(want) {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestAScheduleThatCanNeverFireSaysSoInsteadOfHanging(t *testing.T) {
	// The 30th of February parses — every field is in range — and no instant satisfies it. The
	// bounded search is what turns that into an answer rather than a spin.
	if got := mustParse(t, "0 0 30 2 *").Next(at(2026, 1, 1, 0, 0)); !got.IsZero() {
		t.Errorf("got %s, want the zero time", got)
	}
	// So does the zero Schedule, which is what a caller holds before parsing anything.
	if got := (Schedule{}).Next(at(2026, 1, 1, 0, 0)); !got.IsZero() {
		t.Errorf("zero Schedule fired at %s", got)
	}
}

// TestNextIsStrictlyAfter is what stops a job firing twice in the minute it fired.
func TestNextIsStrictlyAfter(t *testing.T) {
	s := mustParse(t, "0 3 * * *")
	fired := at(2026, 8, 10, 3, 0)
	if got := s.Next(fired); !got.After(fired) {
		t.Fatalf("Next(%s) = %s, which is not after it", fired, got)
	}
	if want := at(2026, 8, 11, 3, 0); !s.Next(fired).Equal(want) {
		t.Errorf("got %s, want %s", s.Next(fired), want)
	}
	// Seconds within the firing minute do not earn a second fire either.
	mid := time.Date(2026, 8, 10, 3, 0, 30, 0, time.UTC)
	if want := at(2026, 8, 11, 3, 0); !s.Next(mid).Equal(want) {
		t.Errorf("from mid-minute: got %s, want %s", s.Next(mid), want)
	}
}

func TestBadSchedulesAreRefusedWithAReason(t *testing.T) {
	bad := []string{
		"",
		"   ",
		"* * * *",      // four fields
		"* * * * * *",  // six
		"60 * * * *",   // minute out of range
		"* 24 * * *",   // hour out of range
		"* * 0 * *",    // day-of-month starts at 1
		"* * * 13 *",   // month out of range
		"* * * * 8",    // day-of-week out of range
		"*/0 * * * *",  // zero step
		"*/-1 * * * *", // negative step
		"5-1 * * * *",  // backwards range, refused rather than wrapped
		// The same backwards range with a sibling term. Worth its own case: alone, "5-1" is caught
		// by the field ending up empty, so the explicit check looks redundant — here the sibling
		// fills the field and the bad term would be dropped in silence without it.
		"5-1,3 * * * *",
		"x * * * *",         // not a number
		"* * * xxx *",       // not a month name
		"1,,2 * * * *",      // empty term
		"@nonesuch",         // unknown shorthand
		"0 0 * * mon-sunny", // name that is not one
	}
	for _, spec := range bad {
		if s, err := Parse(spec); err == nil {
			t.Errorf("Parse(%q) accepted it as %v", spec, s)
		}
	}
}

func TestTheScheduleRemembersHowItWasWritten(t *testing.T) {
	// The UIs show the spec back to the person who wrote it, so it has to survive parsing.
	if got := mustParse(t, "  0 3 * * 1-5  ").String(); got != "0 3 * * 1-5" {
		t.Errorf("String() = %q", got)
	}
}

// The two daylight-saving behaviours the package comment claims. Pinned here so they stay
// deliberate: both are consequences of "move forward in absolute time, match on the wall clock",
// and a change to the search loop that quietly altered either would otherwise go unnoticed.

func TestAnHourThatDoesNotExistDoesNotFire(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("no tzdata: %v", err)
	}
	// 2026-03-08: clocks jump 02:00 -> 03:00, so 02:30 never happens that day.
	s := mustParse(t, "30 2 * * *")
	got := s.Next(time.Date(2026, 3, 7, 12, 0, 0, 0, ny))
	want := time.Date(2026, 3, 9, 2, 30, 0, 0, ny)
	if !got.Equal(want) {
		t.Errorf("got %s, want %s (the 8th has no 02:30)", got.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}

func TestAnHourThatHappensTwiceFiresTwice(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("no tzdata: %v", err)
	}
	// 2026-11-01: clocks fall back 02:00 -> 01:00, so 01:30 happens at two distinct instants.
	s := mustParse(t, "30 1 * * *")
	first := s.Next(time.Date(2026, 11, 1, 0, 0, 0, 0, ny))
	second := s.Next(first)
	if first.Hour() != 1 || first.Minute() != 30 {
		t.Fatalf("first fire %s is not 01:30", first.Format(time.RFC3339))
	}
	if second.Hour() != 1 || second.Minute() != 30 || second.Day() != 1 {
		t.Fatalf("second fire %s is not the repeated 01:30 on the same day", second.Format(time.RFC3339))
	}
	if !second.After(first) {
		t.Fatalf("second fire %s is not after the first %s", second, first)
	}
	// Same wall clock, one hour apart in absolute time — which is exactly what a repeated hour is.
	if d := second.Sub(first); d != time.Hour {
		t.Errorf("the two fires are %s apart, want 1h", d)
	}
}

// A schedule whose hour does NOT match the repeated fall-back hour still terminates — it used to
// spin forever, because the coarse hour-skip landed on the earlier ambiguous instant and never
// advanced in absolute time. An ordinary "3am daily" evaluated during the early hours of the
// fall-back day is the everyday case that hung the cron goroutine.
func TestAnOrdinaryScheduleDoesNotHangOnTheFallBackDay(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("no tzdata: %v", err)
	}
	s := mustParse(t, "0 3 * * *") // 3am daily — hour 3 is not the repeated 1am
	done := make(chan time.Time, 1)
	go func() { done <- s.Next(time.Date(2026, 11, 1, 0, 0, 0, 0, ny)) }()
	select {
	case got := <-done:
		if got.Hour() != 3 || got.Minute() != 0 || got.Day() != 1 {
			t.Fatalf("next fire %s is not 03:00 on the fall-back day", got.Format(time.RFC3339))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Next did not return within 2s — the fall-back hour-skip is spinning")
	}
}

func TestTheSearchSkipsWholeMonthsAndDaysRatherThanCrawling(t *testing.T) {
	// Counted, not timed. The first version of this test gave Next two seconds of wall clock and
	// passed with the coarse skipping removed — crawling five years a minute at a time is ~2.6M
	// cheap iterations and still finishes well inside any timeout loose enough not to flake. So it
	// asserted nothing. Steps are deterministic and are the property actually being kept.
	const ceiling = 5000 // five years of month/day/hour hops; crawling would be ~2,600,000

	// The worst case: a schedule that never matches, so the search runs to its bound.
	if _, steps := mustParse(t, "0 0 30 2 *").next(at(2026, 1, 1, 0, 0)); steps > ceiling {
		t.Errorf("a never-firing schedule took %d steps (ceiling %d) — the month/day skipping is gone", steps, ceiling)
	}
	// And a real but distant one, which is the case a running daemon actually pays for.
	if _, steps := mustParse(t, "0 0 1 1 *").next(at(2026, 1, 2, 0, 0)); steps > ceiling {
		t.Errorf("a yearly schedule took %d steps (ceiling %d)", steps, ceiling)
	}
}
