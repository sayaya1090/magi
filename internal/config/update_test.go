package config

import "testing"

// Auto defaults ON (nil): the operator chose automatic rollout, so a daemon updates itself unless the
// toggle is explicitly turned off.
func TestUpdateAutoDefaultsOnAndRespectsTheToggle(t *testing.T) {
	if !(UpdateConfig{}).AutoOn() {
		t.Error("auto should default on (nil)")
	}
	off := false
	if (UpdateConfig{Auto: &off}).AutoOn() {
		t.Error("auto = false should be off")
	}
	on := true
	if !(UpdateConfig{Auto: &on}).AutoOn() {
		t.Error("auto = true should be on")
	}
}
