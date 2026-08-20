package lua

import (
	"strings"
	"testing"

	lua "github.com/yuin/gopher-lua"
)

// The encoder's whole point is that the bytes do not move. A tool schema is the head of every
// prompt a CLI shim sends, so an encoder that walks a table in hash order makes the head differ on
// every request and every prefix-cache lookup miss — measured, across three bench arms that read
// zero from the cache.
func TestJSONEncodeIsTheSameBytesEveryTime(t *testing.T) {
	out, err := runLua(t, `
local schema = { type = "object", properties = {
  command = { type = "string" }, timeout = { type = "integer" }, background = { type = "boolean" },
  cwd = { type = "string" }, env = { type = "object" }, stdin = { type = "string" },
  id = { type = "string" }, eof = { type = "boolean" }, label = { type = "string" } },
  required = { "command" } }
local first = magi.json_encode(schema)
for i = 1, 40 do
  if magi.json_encode(schema) ~= first then error("encoding " .. i .. " differed") end
end
magi.log(first)`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"properties":{"background"`) {
		t.Errorf("keys are not sorted: %s", out)
	}
}

// It must also agree with the hand-written Lua the shims are moving off, byte for byte — a shim
// that switches must not change one byte of its rendered prompt, or the first turn after the change
// pays a full cache write for nothing.
func TestJSONEncodeMatchesTheLuaItReplaces(t *testing.T) {
	out, err := runLua(t, luaJenc+`
local cases = {
  { a = 1, b = "two", c = true },
  { 1, 2, 3 },
  {},
  { nested = { list = { "x", "y" }, n = 1.5, esc = "a\"b\\c\nd\te" } },
  { type = "object", properties = { p = { type = "string" } }, required = { "p" } },
}
for i, c in ipairs(cases) do
  local mine, theirs = magi.json_encode(c), jenc(c)
  if mine ~= theirs then error("case " .. i .. ": bridge " .. mine .. " vs lua " .. theirs) end
end
magi.log("all cases match")`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "all cases match") {
		t.Errorf("comparison did not run: %s", out)
	}
}

// luaJenc is the encoder as the shims wrote it, kept here as the reference the bridge is checked
// against. When the shims drop their copies this stays, because "the same bytes as before" needs a
// before.
const luaJenc = `
local function jstr(s)
  s = s:gsub('\\', '\\\\'):gsub('"', '\\"')
  s = s:gsub('\n', '\\n'):gsub('\r', '\\r'):gsub('\t', '\\t')
  s = s:gsub('%c', function(c) return string.format('\\u%04x', string.byte(c)) end)
  return '"' .. s .. '"'
end
function jenc(v)
  local t = type(v)
  if t == 'string' then return jstr(v) end
  if t == 'number' then
    if v == math.floor(v) then return string.format('%d', v) end
    return tostring(v)
  end
  if t == 'boolean' then return v and 'true' or 'false' end
  if t == 'table' then
    local n = 0
    for _ in pairs(v) do n = n + 1 end
    if n == #v then
      local parts = {}
      for i = 1, #v do parts[#parts + 1] = jenc(v[i]) end
      return '[' .. table.concat(parts, ',') .. ']'
    end
    local keys = {}
    for k in pairs(v) do keys[#keys + 1] = k end
    table.sort(keys)
    local parts = {}
    for _, k in ipairs(keys) do parts[#parts + 1] = jstr(k) .. ':' .. jenc(v[k]) end
    return '{' .. table.concat(parts, ',') .. '}'
  end
  return 'null'
end
`

func runLua(t *testing.T, src string) (string, error) {
	t.Helper()
	var logged strings.Builder
	p := &plugin{logf: func(s string) { logged.WriteString(s + "\n") }}
	p.L = newSandbox()
	installBridge(p)
	err := p.L.DoString(src)
	p.L.Close()
	_ = lua.LNil
	return logged.String(), err
}
