package builtin

import (
	"regexp"
	"strings"
	"testing"
)

// The pattern the guard could not read was the one that killed the run.
//
// A trial rebuilding Caffe swept its stale compilers with
//
//	pkill -9 make; pkill -9 cmake; pkill -9 g++; pkill -9 cc1plus
//
// and ended in a non-zero exit. Go's regexp refuses `g++` ("invalid nested repetition operator"),
// and the guard then asked whether any process name CONTAINS the literal "g++" — none does — and
// let it through. Measured 2026-08-18: on Linux `echo magi | grep -E 'g++'` matches, because glibc
// and busybox read the second + as a repeat of `g+`. The binary under test is magi-amd64.
func TestAPatternGoCannotReadIsNotAssumedHarmless(t *testing.T) {
	if _, err := regexp.Compile("g++"); err == nil {
		t.Skip("Go now accepts g++; this test guards the fallback that exists because it does not")
	}
	why := selfKillReason("pkill -9 g++", "/tmp/magi-serve/magi-amd64 -p build caffe", "magi-amd64")
	if why == "" {
		t.Error("`pkill -9 g++` was allowed against a process named magi-amd64")
	}
	if !strings.Contains(why, "blocked") {
		t.Errorf("the refusal does not say it blocked anything: %q", why)
	}
}

// Conservative is not indiscriminate. The same sweep's other patterns have nothing to do with us
// and must still run, or the guard becomes the thing that stops work.
func TestTheRestOfThatSweepStillRuns(t *testing.T) {
	for _, cmd := range []string{"pkill -9 make", "pkill -9 cmake", "pkill -9 cc1plus", "pkill -9 c++"} {
		if why := selfKillReason(cmd, "/tmp/magi-serve/magi-amd64 -p x", "magi-amd64"); why != "" {
			t.Errorf("%s was refused, and it cannot reach us: %s", cmd, why)
		}
	}
}

// A doubled quantifier means what the single one means to every engine that accepts it, so it is
// read that way before anything cruder is tried.
func TestDoubledQuantifiersAreReadTheWayPkillReadsThem(t *testing.T) {
	if got := collapseQuantifiers("g++"); got != "g+" {
		t.Errorf("collapseQuantifiers(\"g++\") = %q", got)
	}
	if got := collapseQuantifiers("a+*?+b"); got != "a+b" {
		t.Errorf("a run of quantifiers collapsed to %q", got)
	}
	if !patternMatches("mag++i", "magi") {
		t.Error("a pattern Go rejects but pkill accepts did not match a name it covers")
	}
}
