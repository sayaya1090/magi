package app

import "testing"

// specMineEnabled defaults OFF: the mined note misled a weak executor (kv-store-grpc `val`/`value`)
// and injected a false premise, so it must be opt-in. An explicit truthy value re-enables it.
func TestSpecMineDefaultOff(t *testing.T) {
	if specMineEnabled() {
		t.Fatal("default must be OFF")
	}
	for _, v := range []string{"1", "on", "true", "yes"} {
		t.Setenv("MAGI_SPEC_MINE", v)
		if !specMineEnabled() {
			t.Errorf("%q must enable", v)
		}
	}
	for _, v := range []string{"0", "off", "false", ""} {
		t.Setenv("MAGI_SPEC_MINE", v)
		if specMineEnabled() {
			t.Errorf("%q must leave it OFF", v)
		}
	}
}
