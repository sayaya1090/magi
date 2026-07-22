package app

import "testing"

// specMineEnabled defaults ON: the mined identifiers now feed the curated brief's verbatim-literal
// defense (the kv-store-grpc `val`/`value` drift it once caused is what that defense targets).
// MAGI_SPEC_MINE=off restores the un-mined baseline.
func TestSpecMineDefaultOn(t *testing.T) {
	if !specMineEnabled() {
		t.Fatal("default must be ON")
	}
	for _, v := range []string{"0", "off", "false"} {
		t.Setenv("MAGI_SPEC_MINE", v)
		if specMineEnabled() {
			t.Errorf("%q must disable", v)
		}
	}
	for _, v := range []string{"1", "on", "true", "yes", ""} {
		t.Setenv("MAGI_SPEC_MINE", v)
		if !specMineEnabled() {
			t.Errorf("%q must leave it ON", v)
		}
	}
}
