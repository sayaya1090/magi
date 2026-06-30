package lua

import (
	"strings"

	"github.com/BurntSushi/toml"
	lua "github.com/yuin/gopher-lua"

	"github.com/sayaya1090/magi/internal/config"
)

// magi.get_config_key / magi.set_config_key read and write the USER's config.toml by a
// dotted key (e.g. "routing.model", "plugins.<name>.token"). Distinct from store_get/set,
// which use the plugin's own isolated JSON store. Writes go through config.SetKey, a
// line-level edit that preserves comments and the rest of the file.
//
// Gating: a plugin may always read/write its OWN section (plugins.<name>.*); anything else
// needs an explicit manifest grant (config:read:<key> / config:write:<key>, trailing * for
// a prefix). A fixed deny-list (mcp / hooks / allow / deny) is blocked even with a grant —
// those sections run commands or change the permission policy.

// ownsConfigKey reports whether key is in this plugin's own [plugins.<name>] section.
func (p *plugin) ownsConfigKey(key string) bool {
	self := "plugins." + p.name
	return key == self || strings.HasPrefix(key, self+".")
}

// magi.get_config_key(key, default?) -> value | default | nil
func (p *plugin) bridgeGetConfigKey(L *lua.LState) int {
	key := L.CheckString(1)
	def := L.Get(2)
	if p.host == nil || p.host.configPath == "" {
		return fail(L, "get_config_key: config path not available")
	}
	if !validConfigKey(key) {
		return fail(L, "get_config_key: invalid key (use dotted [A-Za-z0-9_-] segments): "+key)
	}
	if configKeyDenied(key) {
		return fail(L, "get_config_key: refused — sensitive section: "+key)
	}
	if !p.ownsConfigKey(key) && !p.perms.allowConfigRead(key) {
		return fail(L, "permission denied: config:read:"+key)
	}
	v, ok := readConfigKey(p.host.configPath, key)
	if !ok {
		L.Push(def)
		return 1
	}
	L.Push(goToLua(L, v))
	return 1
}

// magi.set_config_key(key, value) -> true | (nil, err)
// value is written as a TOML string (config.SetKey is a flat string edit); an empty value
// removes the key.
func (p *plugin) bridgeSetConfigKey(L *lua.LState) int {
	key := L.CheckString(1)
	val := L.CheckString(2)
	if p.host == nil || p.host.configPath == "" {
		return fail(L, "set_config_key: config path not available")
	}
	if !validConfigKey(key) {
		return fail(L, "set_config_key: invalid key (use dotted [A-Za-z0-9_-] segments): "+key)
	}
	if configKeyDenied(key) {
		return fail(L, "set_config_key: refused — sensitive section: "+key)
	}
	if !p.ownsConfigKey(key) && !p.perms.allowConfigWrite(key) {
		return fail(L, "permission denied: config:write:"+key)
	}
	section, leaf := splitConfigKey(key)
	if leaf == "" {
		return fail(L, "set_config_key: invalid key "+key)
	}
	if err := config.SetKey(p.host.configPath, section, leaf, val); err != nil {
		return fail(L, "set_config_key: "+err.Error())
	}
	p.logf("[" + p.name + "] set config " + key)
	L.Push(lua.LTrue)
	return 1
}

// splitConfigKey splits a dotted key into the TOML table section (all but the last segment)
// and the leaf key. "a.b.c" → ("a.b","c"); "model" → ("","model").
func splitConfigKey(key string) (section, leaf string) {
	if i := strings.LastIndexByte(key, '.'); i >= 0 {
		return key[:i], key[i+1:]
	}
	return "", key
}

// readConfigKey decodes config.toml and navigates the dotted key. Returns (value, true) on
// a hit; (nil, false) on any decode error or missing segment.
func readConfigKey(path, key string) (any, bool) {
	var m map[string]any
	if _, err := toml.DecodeFile(path, &m); err != nil {
		return nil, false
	}
	cur := any(m)
	for _, seg := range strings.Split(key, ".") {
		mp, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		v, ok := mp[seg]
		if !ok {
			return nil, false
		}
		cur = v
	}
	return cur, true
}
