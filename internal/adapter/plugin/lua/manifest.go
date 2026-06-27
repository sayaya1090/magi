// Package lua implements a hot-reloadable plugin host backed by gopher-lua
// (pure Go, no CGo — preserves cross-compilation). A plugin is a directory with
// a plugin.toml manifest and a Lua entry script that registers capabilities
// (M3: tools) through a sandboxed `magi.*` bridge. (D10, SPEC F-PLUGIN)
package lua

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Manifest is the declared metadata and permission grant for a plugin.
type Manifest struct {
	Name         string   `toml:"name"`
	Version      string   `toml:"version"`
	Description  string   `toml:"description"`
	Entry        string   `toml:"entry"`        // default "init.lua"
	Capabilities []string `toml:"capabilities"` // e.g. ["tool"]
	Permissions  []string `toml:"permissions"`  // e.g. ["fs:read:./", "exec:git"]
}

// loadManifest reads and validates <dir>/plugin.toml.
func loadManifest(dir string) (Manifest, error) {
	var m Manifest
	path := filepath.Join(dir, "plugin.toml")
	b, err := os.ReadFile(path)
	if err != nil {
		return m, fmt.Errorf("plugin: read manifest: %w", err)
	}
	if err := toml.Unmarshal(b, &m); err != nil {
		return m, fmt.Errorf("plugin: parse manifest %s: %w", path, err)
	}
	if m.Name == "" {
		return m, fmt.Errorf("plugin: manifest missing name (%s)", path)
	}
	if m.Entry == "" {
		m.Entry = "init.lua"
	}
	return m, nil
}
