package app

import "testing"

// soloAuditEnabled is ON by default: only an explicit falsey MAGI_SOLO_AUDIT restores the
// >=2-step-only plan audit (the A/B knob). Mirrors the specFidelity/orient flag test shape.
func TestSoloAuditEnabledFlag(t *testing.T) {
	for _, v := range []string{"", "1", "on", "true", "yes", "ON", "garbage"} {
		t.Setenv("MAGI_SOLO_AUDIT", v)
		if !soloAuditEnabled() {
			t.Errorf("MAGI_SOLO_AUDIT=%q should enable solo audit (default is on)", v)
		}
	}
	for _, v := range []string{"0", "off", "false", "no", "OFF"} {
		t.Setenv("MAGI_SOLO_AUDIT", v)
		if soloAuditEnabled() {
			t.Errorf("MAGI_SOLO_AUDIT=%q should NOT enable", v)
		}
	}
}
