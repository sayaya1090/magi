package envflag

import (
	"os"
	"testing"
)

// The whole point of one reader is that every switch answers to the same words. These are the
// values the docs and the flag comments promise, plus the ones that used to be honoured by some
// readers and ignored by others.
func TestEnabledReadsEveryFormOfYesAndNo(t *testing.T) {
	const name = "MAGI_TEST_SWITCH"

	for _, v := range []string{"0", "off", "false", "no", "OFF", "No", " false "} {
		t.Setenv(name, v)
		if Enabled(name, true) {
			t.Errorf("%q must turn a default-on switch off", v)
		}
		if Enabled(name, false) {
			t.Errorf("%q must leave a default-off switch off", v)
		}
	}
	for _, v := range []string{"1", "on", "true", "yes", "ON", "Yes", " true "} {
		t.Setenv(name, v)
		if !Enabled(name, false) {
			t.Errorf("%q must turn a default-off switch on", v)
		}
		if !Enabled(name, true) {
			t.Errorf("%q must leave a default-on switch on", v)
		}
	}
	// Unset, empty, and unrecognized all leave the default. A misspelled value must not be read
	// as either answer — silently flipping a mechanism on a typo is worse than ignoring it.
	for _, v := range []string{"", "  ", "maybe", "2", "of"} {
		t.Setenv(name, v)
		if !Enabled(name, true) || Enabled(name, false) {
			t.Errorf("%q must leave the default untouched", v)
		}
	}
	t.Setenv(name, "x") // registers the cleanup that restores the original environment
	if err := os.Unsetenv(name); err != nil {
		t.Fatalf("unsetenv: %v", err)
	}
	if !Enabled(name, true) || Enabled(name, false) {
		t.Error("an unset switch must leave the default untouched")
	}
}
