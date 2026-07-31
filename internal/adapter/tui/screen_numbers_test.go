package tui

import (
	"strings"
	"testing"
	"time"
)

// Two small renderers put a NUMBER and a TIME on screen where the user reads them as fact: the
// token counts under /cost, and how long ago each session in the resume picker was touched. Both
// have a stated contract, and both fail as a plausible-looking wrong value rather than as an error.

// humanCount shortens a token count. The boundaries are where a wrong unit would make a bill read
// a thousand times off and still look reasonable.
func TestTokenCountsReadAsWhatTheyAre(t *testing.T) {
	for _, tc := range []struct {
		n    int
		want string
	}{
		{0, "0"},
		{1, "1"},
		{999, "999"},   // still exact below the k boundary
		{1000, "1.0k"}, // …and switches exactly at it
		{12345, "12.3k"},
		{999999, "1000.0k"},
		{1_000_000, "1.00M"}, // the M boundary, not 1000.0k
		{2_500_000, "2.50M"},
	} {
		if got := humanCount(tc.n); got != tc.want {
			t.Errorf("humanCount(%d) = %q; want %q", tc.n, got, tc.want)
		}
	}
	// Never bare-negative-formatted into a unit: a negative count is a bug upstream, but it must
	// not render as a plausible size.
	if got := humanCount(-5); got != "-5" {
		t.Errorf("humanCount(-5) = %q", got)
	}
}

// relAge answers "which one was I just in" for the resume picker. Its contract is that anything it
// cannot state compactly comes back EMPTY so the caller falls back to an absolute stamp — a zero
// time from legacy metadata rendering as "56 years ago" would be a confident wrong answer.
func TestRelativeAgeIsEmptyWhenItCannotSayItCompactly(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		what string
		at   time.Time
		want string
	}{
		{"zero time (legacy metadata)", time.Time{}, ""},
		{"in the future (clock skew)", now.Add(time.Hour), ""},
		{"older than a week", now.Add(-8 * 24 * time.Hour), ""},
		{"exactly a week", now.Add(-7 * 24 * time.Hour), ""},
	} {
		if got := relAge(tc.at); got != tc.want {
			t.Errorf("%s: relAge = %q; want the caller to fall back (empty)", tc.what, got)
		}
	}
	// …and within the week it says so, in the unit that fits.
	for _, tc := range []struct {
		at   time.Time
		unit string
	}{
		{now.Add(-42 * time.Second), "s ago"},
		{now.Add(-5 * time.Minute), "m ago"},
		{now.Add(-3 * time.Hour), "h ago"},
		{now.Add(-6 * 24 * time.Hour), "d ago"},
	} {
		got := relAge(tc.at)
		if !strings.HasSuffix(got, tc.unit) {
			t.Errorf("relAge(%v) = %q; want it to end in %q", tc.at.Format(time.RFC3339), got, tc.unit)
		}
		if strings.HasPrefix(got, "-") {
			t.Errorf("relAge produced a negative age: %q", got)
		}
	}
}
