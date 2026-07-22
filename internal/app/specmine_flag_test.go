package app

import "testing"

// specMineEnabled defaults OFF: the mined note re-primes the executor and council with a verbatim
// literal (kv-store-grpc: "honor kv-store" → council rejected the correct underscore pb2 files and
// drove a hyphen rename into a Python SyntaxError, thrashing a task the model used to solve). It is
// opt-in for A/B; an explicit truthy value re-enables it.
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
