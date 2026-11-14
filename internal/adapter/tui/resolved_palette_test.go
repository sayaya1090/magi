package tui

import "testing"

// A viewer outside the terminal gets the same palette the terminal paints with: the built-in
// roles with the person's [theme] laid over them, empty values ignored, the defaults untouched.
func TestResolvedPaletteLaysTheConfigOverTheDefaults(t *testing.T) {
	dark := ResolvedPalette(true, map[string]string{"primary": "#010203", "accent": ""})
	if dark["primary"] != "#010203" {
		t.Errorf("override lost: %q", dark["primary"])
	}
	if dark["accent"] != nervDark["accent"] || dark["surfaceContainer"] != nervDark["surfaceContainer"] {
		t.Errorf("defaults not kept: accent %q surfaceContainer %q", dark["accent"], dark["surfaceContainer"])
	}
	if nervDark["primary"] == "#010203" {
		t.Fatal("the built-in palette was modified")
	}
	if light := ResolvedPalette(false, nil); light["primary"] != nervLight["primary"] {
		t.Errorf("light default %q", light["primary"])
	}
}
