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

// constraintGateEnabled defaults OFF (opt-in): the scope/boundary rejection clause adds a reason for an
// already over-strict council to reject, so it ships off and is measured on an A/B arm before default.
func TestConstraintGateEnabledDefaultOff(t *testing.T) {
	if constraintGateEnabled() {
		t.Fatal("default must be OFF (opt-in)")
	}
	for _, v := range []string{"1", "on", "true", "yes"} {
		t.Setenv("MAGI_CONSTRAINT_GATE", v)
		if !constraintGateEnabled() {
			t.Errorf("%q must enable it", v)
		}
	}
	for _, v := range []string{"0", "off", "false", "", "no"} {
		t.Setenv("MAGI_CONSTRAINT_GATE", v)
		if constraintGateEnabled() {
			t.Errorf("%q must leave it OFF", v)
		}
	}
}
