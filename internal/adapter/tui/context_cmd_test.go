package tui

import (
	"regexp"
	"strings"
	"testing"
)

// /context is the one slash command that takes a NUMBER from the user and changes a setting with
// it, so the parse is the whole surface: a size read wrong is not a visible error, it is a window
// silently set to something else.
func TestTheContextSizesAUserWouldTypeParseToWhatTheySay(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int
		ok   bool
	}{
		{"128000", 128000, true},
		{"128k", 128000, true},
		{"128K", 128000, true}, // typed with caps
		{" 64k ", 64000, true}, // pasted with whitespace
		{"1m", 1_000_000, true},
		{"1M", 1_000_000, true},
		// The words that mean "do not cap this" all land on the same value, which is what the
		// setter renders as "unlimited".
		{"unlimited", 0, true},
		{"none", 0, true},
		{"off", 0, true},
		{"auto", 0, true},
		{"0", 0, true},
		{"0k", 0, true},
		// Refused rather than guessed at. "1.5k" is the interesting one: rounding it silently
		// would set a window the user never asked for.
		{"1.5k", 0, false},
		{"128kb", 0, false},
		{"k", 0, false},
		{"m", 0, false},
		{"-5", 0, false},
		{"-5k", 0, false},
		{"abc", 0, false},
		{"", 0, false},
	} {
		got, ok := parseTokenCount(tc.in)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("%q → (%d, %v); want (%d, %v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

// A size the parse cannot represent must not reach the setter as a real number. The multiply by
// 1000/1_000_000 overflows past roughly 9.2e15, and the product comes back NEGATIVE — the setter
// clamps that to zero and honestly reports "unlimited", which is why this is a pin on the landing
// rather than a bug report: an absurd number is answered as no limit, not as a negative window.
func TestAnUnrepresentableSizeStillLandsHonestly(t *testing.T) {
	s := newScript(t)
	s.m.app.SetModel(s.m.sid, "a-model") // as above: the fixture's session starts without one
	s.typeText("/context 9999999999999999k").enter()
	if got := s.m.snackbar; !strings.Contains(got, "unlimited") && !strings.Contains(got, "usage:") {
		t.Errorf("an unrepresentable size reported %q — neither a limit the user can read nor a refusal", got)
	}
	// A minus sign in front of a DIGIT — the model name has a hyphen in it, and matching that
	// would be the assertion failing on its own fixture rather than on the behaviour.
	if regexp.MustCompile(`-\d`).MatchString(s.m.snackbar) {
		t.Errorf("a negative window reached the user: %q", s.m.snackbar)
	}
}

// The three shapes of the command, through the real key path. Each has to say what it did: bare
// /context prints the usage view as a transcript block, the one- and two-argument forms answer in
// the snackbar with the model they changed.
func TestEveryShapeOfTheContextCommandAnswers(t *testing.T) {
	t.Run("bare prints the view", func(t *testing.T) {
		s := newScript(t)
		before := len(s.m.blocks)
		s.typeText("/context").enter()
		if len(s.m.blocks) <= before {
			t.Errorf("bare /context added no block; snackbar says %q", s.m.snackbar)
		}
	})
	t.Run("one argument sets the session model", func(t *testing.T) {
		s := newScript(t)
		// The fixture creates a session with no model, which cannot happen in a real run — magi
		// always starts one with a model — and /context would honestly refuse. Give it one so the
		// path under test is the one a user reaches.
		s.m.app.SetModel(s.m.sid, "a-model")
		s.typeText("/context 128k").enter()
		if got := s.m.snackbar; !strings.Contains(got, "128,000") && !strings.Contains(got, "128000") {
			t.Errorf("the snackbar does not report the size that was set: %q", got)
		}
	})
	t.Run("two arguments name the model", func(t *testing.T) {
		s := newScript(t)
		s.typeText("/context some-other-model 200k").enter()
		if got := s.m.snackbar; !strings.Contains(got, "some-other-model") {
			t.Errorf("the snackbar does not name the model it changed: %q", got)
		}
	})
	t.Run("a size it cannot read is refused with the usage", func(t *testing.T) {
		s := newScript(t)
		s.typeText("/context 1.5k").enter()
		if got := s.m.snackbar; !strings.Contains(got, "usage:") {
			t.Errorf("an unreadable size was not refused: %q", got)
		}
	})
}
