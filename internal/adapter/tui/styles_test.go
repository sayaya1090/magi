package tui

import "testing"

func TestResolvePaletteDefaults(t *testing.T) {
	SetThemePalettes(nil, nil) // no overrides → built-in NERV/MAGI defaults
	if got := resolvePalette(true)["primary"]; got != nervDark["primary"] {
		t.Errorf("dark primary = %q, want default %q", got, nervDark["primary"])
	}
	if got := resolvePalette(false)["primary"]; got != nervLight["primary"] {
		t.Errorf("light primary = %q, want default %q", got, nervLight["primary"])
	}
}

func TestResolvePaletteMergesOverrides(t *testing.T) {
	SetThemePalettes(
		map[string]string{"primary": "#010203", "accent": ""}, // dark: override primary, ignore empty accent
		map[string]string{"surface": "#fefefe"},               // light: override surface only
	)
	defer SetThemePalettes(nil, nil) // don't leak override into other tests

	dark := resolvePalette(true)
	if dark["primary"] != "#010203" {
		t.Errorf("dark primary override = %q, want #010203", dark["primary"])
	}
	if dark["accent"] != nervDark["accent"] {
		t.Errorf("empty override should be ignored: accent = %q, want default %q", dark["accent"], nervDark["accent"])
	}
	if dark["surface"] != nervDark["surface"] {
		t.Errorf("unspecified role should keep default: surface = %q", dark["surface"])
	}

	light := resolvePalette(false)
	if light["surface"] != "#fefefe" {
		t.Errorf("light surface override = %q, want #fefefe", light["surface"])
	}
	if light["primary"] != nervLight["primary"] {
		t.Errorf("light primary should keep default = %q", light["primary"])
	}
}
