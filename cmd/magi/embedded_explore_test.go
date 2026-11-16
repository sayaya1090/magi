package main

import (
	"strings"
	"testing"

	"github.com/sayaya1090/magi/plugins"
)

// explore ships in the binary and is on unless a config switches it off.
func TestEmbeddedExploreShipsOnByDefault(t *testing.T) {
	p, ok := plugins.Embedded["explore"]
	if !ok || !p.DefaultOn {
		t.Fatalf("explore is not embedded on by default: %v %+v", ok, p)
	}
	manifest, err := p.FS.ReadFile("explore/plugin.toml")
	if err != nil || !strings.Contains(string(manifest), `name = "explore"`) {
		t.Fatalf("explore/plugin.toml: %v", err)
	}
	if _, err := p.FS.ReadFile("explore/init.lua"); err != nil {
		t.Fatalf("explore/init.lua: %v", err)
	}
}
