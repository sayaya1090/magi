package app

import "testing"

// councilKeepEnabled defaults ON: an explicit off-value restores the baseline (no keep clause).
func TestCouncilKeepEnabledDefault(t *testing.T) {
	if !councilKeepEnabled() {
		t.Fatal("default must be ON")
	}
	for _, v := range []string{"0", "off", "false", "no", "OFF"} {
		t.Setenv("MAGI_COUNCIL_KEEP", v)
		if councilKeepEnabled() {
			t.Errorf("%q must disable", v)
		}
	}
	for _, v := range []string{"1", "on", "true", "yes", "", "whatever"} {
		t.Setenv("MAGI_COUNCIL_KEEP", v)
		if !councilKeepEnabled() {
			t.Errorf("%q must leave it ON", v)
		}
	}
}
