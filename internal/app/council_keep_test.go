package app

import "testing"

// councilKeepEnabled defaults OFF (A/B): the keep advisory ships only when explicitly
// enabled, so the baseline council prompt/notes are unchanged.
func TestCouncilKeepEnabledDefault(t *testing.T) {
	if councilKeepEnabled() {
		t.Fatal("default must be OFF")
	}
	for _, v := range []string{"1", "on", "true", "yes", "ON"} {
		t.Setenv("MAGI_COUNCIL_KEEP", v)
		if !councilKeepEnabled() {
			t.Errorf("%q must enable", v)
		}
	}
	for _, v := range []string{"0", "off", "false", "no", ""} {
		t.Setenv("MAGI_COUNCIL_KEEP", v)
		if councilKeepEnabled() {
			t.Errorf("%q must leave it OFF", v)
		}
	}
}
