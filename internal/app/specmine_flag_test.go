package app

import "testing"

// specMineEnabled defaults ON, with a tool-derived-name guard in the prompt and note so the mined
// identifiers no longer push "match the raw spelling" onto names a tool derives (the kv-store-grpc
// hyphen→underscore thrash). MAGI_SPEC_MINE=off restores the un-mined baseline for A/B.
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
