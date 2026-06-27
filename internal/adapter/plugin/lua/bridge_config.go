package lua

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	lua "github.com/yuin/gopher-lua"
)

// Plugin config: a plugin reads settings the user placed under [plugins.<name>]
// in config.toml, and persists its own values (e.g. a per-user config downloaded
// from a backend) to an isolated JSON store at <dataDir>/plugin-data/<name>.json.
// Read precedence: persisted store > config.toml section > caller default.

var pluginStoreMu sync.Mutex // serializes store file reads/writes across plugins

// storePath returns this plugin's persistent store file, or "" if no data dir.
func (p *plugin) storePath() string {
	if p.host == nil || p.host.dataDir == "" {
		return ""
	}
	return filepath.Join(p.host.dataDir, "plugin-data", p.name+".json")
}

// readStore loads the plugin's persisted JSON store (empty map if absent).
func (p *plugin) readStore() map[string]any {
	path := p.storePath()
	if path == "" {
		return map[string]any{}
	}
	pluginStoreMu.Lock()
	defer pluginStoreMu.Unlock()
	b, err := os.ReadFile(path)
	if err != nil {
		return map[string]any{}
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil || m == nil {
		return map[string]any{}
	}
	return m
}

// magi.config_get(key, default?) -> value | default | nil
// Reads from the persisted store, then the config.toml [plugins.<name>] section,
// then returns the caller's default (or nil).
func (p *plugin) bridgeConfigGet(L *lua.LState) int {
	key := L.CheckString(1)
	def := L.Get(2) // may be LNil

	if v, ok := p.readStore()[key]; ok {
		L.Push(goToLua(L, v))
		return 1
	}
	if p.host != nil {
		if sec := p.host.pluginConfigs[p.name]; sec != nil {
			if v, ok := sec[key]; ok {
				L.Push(goToLua(L, v))
				return 1
			}
		}
	}
	L.Push(def)
	return 1
}

// magi.config_set(key, value) -> true | (nil, err)
// Persists a value to the plugin's store so it survives restarts. Used to save a
// per-user config downloaded from a backend service.
func (p *plugin) bridgeConfigSet(L *lua.LState) int {
	key := L.CheckString(1)
	val := luaToGo(L.Get(2))

	path := p.storePath()
	if path == "" {
		return fail(L, "config_set: no data dir configured")
	}

	pluginStoreMu.Lock()
	defer pluginStoreMu.Unlock()
	m := map[string]any{}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &m)
		if m == nil {
			m = map[string]any{}
		}
	}
	m[key] = val
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fail(L, "config_set: "+err.Error())
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fail(L, "config_set: "+err.Error())
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return fail(L, "config_set: "+err.Error())
	}
	L.Push(lua.LTrue)
	return 1
}
