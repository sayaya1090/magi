package lua

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/sayaya1090/magi/internal/atomicfile"
	"github.com/sayaya1090/magi/internal/filelock"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

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
	b, err := atomicfile.ReadFile(path)
	if err != nil {
		return map[string]any{}
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil || m == nil {
		return map[string]any{}
	}
	return m
}

// magi.store_get(key, default?) -> value | default | nil
// Reads from the persisted store, then the config.toml [plugins.<name>] section,
// then returns the caller's default (or nil).
func (p *plugin) bridgeStoreGet(L *lua.LState) int {
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

// magi.store_prune_fields(prefix, paths) drops fields from persisted keys with that prefix.
func (p *plugin) bridgeStorePruneFields(L *lua.LState) int {
	prefix := L.CheckString(1)
	spec := L.CheckTable(2)
	if prefix == "" {
		return fail(L, "store_prune_fields: prefix is required")
	}
	paths := make([][]string, 0, spec.Len())
	for i := 1; i <= spec.Len(); i++ {
		path, ok := spec.RawGetInt(i).(*lua.LTable)
		if !ok || path.Len() == 0 || path.Len() > 8 {
			return fail(L, "store_prune_fields: invalid field path")
		}
		fields := make([]string, path.Len())
		for j := range fields {
			field, ok := path.RawGetInt(j + 1).(lua.LString)
			if !ok || field == "" {
				return fail(L, "store_prune_fields: invalid field name")
			}
			fields[j] = string(field)
		}
		paths = append(paths, fields)
	}
	err := p.updateStore(func(m map[string]any) (bool, error) {
		changed := false
		for key, value := range m {
			if strings.HasPrefix(key, prefix) {
				for _, path := range paths {
					if pruneStoreField(value, path) {
						changed = true
					}
				}
			}
		}
		return changed, nil
	})
	if err != nil {
		return fail(L, err.Error())
	}
	L.Push(lua.LTrue)
	return 1
}

func pruneStoreField(value any, path []string) bool {
	if path[0] == "*" && len(path) > 1 {
		items, _ := value.([]any)
		changed := false
		for _, item := range items {
			if pruneStoreField(item, path[1:]) {
				changed = true
			}
		}
		return changed
	}
	object, ok := value.(map[string]any)
	if !ok {
		return false
	}
	if len(path) > 1 {
		return pruneStoreField(object[path[0]], path[1:])
	}
	_, found := object[path[0]]
	delete(object, path[0])
	return found
}

// magi.store_set(key, value) -> true | (nil, err)
// Persists a value to the plugin's store so it survives restarts. Used to save a
// per-user config downloaded from a backend service.
func (p *plugin) bridgeStoreSet(L *lua.LState) int {
	key := L.CheckString(1)
	val := luaToGo(L.Get(2))
	if err := p.writeStoreValue(key, val, nil); err != nil {
		return fail(L, err.Error())
	}
	L.Push(lua.LTrue)
	return 1
}

// Compare and replace one serialized state under the same cross-process lock as
// ordinary store_set. A stale Lua read cannot overwrite another writer's queue.
func (p *plugin) bridgeStoreCompareExchange(L *lua.LState) int {
	key := L.CheckString(1)
	expected := L.CheckString(2)
	value := L.CheckString(3)
	if err := p.writeStoreValue(key, value, &expected); err != nil {
		return fail(L, err.Error())
	}
	L.Push(lua.LTrue)
	return 1
}

func (p *plugin) writeStoreValue(key string, val any, expected *string) error {

	return p.updateStore(func(m map[string]any) (bool, error) {
		if expected != nil {
			current := ""
			if v, ok := m[key]; ok {
				var valid bool
				current, valid = v.(string)
				if !valid {
					return false, fmt.Errorf("plugin state is not a string")
				}
			}
			if current != *expected {
				return false, fmt.Errorf("plugin state revision conflict")
			}
		}
		m[key] = val
		return true, nil
	})
}

func (p *plugin) updateStore(update func(map[string]any) (bool, error)) error {
	path := p.storePath()
	if path == "" {
		return fmt.Errorf("store_set: no data dir configured")
	}

	pluginStoreMu.Lock()
	defer pluginStoreMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	release, err := filelock.Acquire(ctx, path+".lock")
	if err != nil {
		return err
	}
	defer release()
	m := map[string]any{}
	if b, err := atomicfile.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &m); err != nil || m == nil {
			return fmt.Errorf("plugin store is invalid; refusing overwrite")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	changed, err := update(m)
	if err != nil || !changed {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicfile.Write(path, b, 0o600); err != nil {
		return err
	}
	return nil
}
