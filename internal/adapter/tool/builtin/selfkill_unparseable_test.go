package builtin

import (
	"regexp"
	"strings"
	"testing"
)

// stubProbe replaces the pgrep call for one test, so the decision is exercised without depending
// on what happens to be running on the machine.
func stubProbe(t *testing.T, hit, ok bool) {
	t.Helper()
	prev := pgrepHitsUs
	pgrepHitsUs = func([]string, string) (bool, bool) { return hit, ok }
	t.Cleanup(func() { pgrepHitsUs = prev })
}

// The general rule: the machine's own matcher decides, and magi does not second-guess it either
// way. A pattern Go would never have matched is still refused when pgrep says it hits us.
func TestTheRealMatcherDecides(t *testing.T) {
	stubProbe(t, true, true)
	why := selfKillReason("pkill -9 somethinggoneverwouldmatch", "/x/magi -p t", "magi")
	if why == "" {
		t.Error("pgrep listed this process and the kill was allowed anyway")
	}
	if !strings.Contains(why, "pgrep lists this process") {
		t.Errorf("the refusal does not say where the answer came from: %q", why)
	}

	// But its silence does not clear anything. Measured on this machine, `pgrep -f builtin.test`
	// finds nothing while a process with exactly that in its argv is running — so a matcher that
	// can miss what is there must not be able to talk the guard out of a refusal the plain check
	// would have made. The oracle adds refusals; it never removes one.
	stubProbe(t, false, true)
	if why := selfKillReason("pkill -9 magi", "/x/magi -p t", "magi"); why == "" {
		t.Error("a miss from pgrep cleared a pattern that plainly covers our own name")
	}
}

// Without pgrep there is no oracle, and the fallback must not repeat the mistake that started
// this: Go's regexp refuses `g++` ("invalid nested repetition operator"), and on Linux that same
// pattern matches any name holding a "g" — magi-amd64 among them. Unreadable is not harmless.
func TestWithoutTheOracleAnUnreadablePatternIsRefused(t *testing.T) {
	if _, err := regexp.Compile("g++"); err == nil {
		t.Skip("Go now reads g++; the fallback this guards exists because it did not")
	}
	stubProbe(t, false, false)
	if why := selfKillReason("pkill -9 g++", "/x/magi-amd64 -p build", "magi-amd64"); why == "" {
		t.Error("`pkill -9 g++` was allowed with no way to check it")
	}
}

// The rest of that sweep has nothing to do with us and must still run, oracle or not.
func TestTheRestOfThatSweepStillRuns(t *testing.T) {
	for _, probe := range []struct {
		name    string
		hit, ok bool
	}{{"oracle says no", false, true}, {"no oracle", false, false}} {
		stubProbe(t, probe.hit, probe.ok)
		for _, cmd := range []string{"pkill -9 make", "pkill -9 cmake", "pkill -9 cc1plus"} {
			if why := selfKillReason(cmd, "/x/magi-amd64 -p x", "magi-amd64"); why != "" {
				t.Errorf("[%s] %s was refused and it cannot reach us: %s", probe.name, cmd, why)
			}
		}
	}
}

// The probe must ask the same question the kill will ask: the signal is dropped, everything that
// changes WHICH PROCESSES MATCH is kept.
func TestOnlyTheSignalIsDroppedFromTheProbe(t *testing.T) {
	for _, f := range []string{"-9", "-KILL", "-SIGKILL", "-15", "-TERM"} {
		if !signalFlag.MatchString(f) {
			t.Errorf("%s is a signal and would have been passed to pgrep as a matching flag", f)
		}
	}
	for _, f := range []string{"-f", "-x", "-i", "-u", "-n", "-o", "--exact"} {
		if signalFlag.MatchString(f) {
			t.Errorf("%s changes what the pattern covers and must reach pgrep", f)
		}
	}
}
