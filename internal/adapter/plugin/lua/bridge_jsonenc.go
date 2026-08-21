package lua

import (
	"sort"
	"strconv"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

// magi.json_encode(v) — JSON out of a Lua value, with the SAME BYTES every time.
//
// The bridge decoded JSON and did not encode it, so every plugin that needed to put a table on a
// wire wrote its own encoder. Three did, each carrying its own copy, because the sandbox has no
// `require` and there is nowhere else to put shared Lua.
//
// Three copies of an encoder is three places to fix, and the fix that mattered had to be made in
// all three: `pairs()` walks a Lua table in HASH order, which varies between calls, so the same
// tool schema encoded twice produced two different byte strings. Those bytes are the head of every
// prompt a shim sends, so the head differed on every request, every prefix-cache lookup missed, and
// three bench arms read ZERO from the cache while other theories were chased. A JSON object is
// unordered by the spec — this is not about correctness, it is about the bytes being STABLE, which
// is what any prefix cache is keyed on.
//
// So the ordering lives here, in one place, with one test, instead of in a comment repeated three
// times and enforced by nobody.
//
// # Matching what the Lua copies produced
//
// The output is byte-identical to those hand-written encoders, deliberately: a shim that switches
// to this must not change one byte of its rendered prompt, or the first turn after the change pays
// a full cache write for nothing. That pins three choices which are otherwise arguable:
//
//   - a table is an ARRAY when its key count equals its length (`n == #v`), and an object
//     otherwise. Lua cannot tell an empty array from an empty map, and the copies called `{}` an
//     array; so does this.
//   - an integral number is written with no decimal point (1, not 1.0), because Lua's `%d` did.
//   - anything that is not a string, number, boolean or table — a function, userdata, nil in a
//     value position — encodes as `null` rather than failing. The copies did that too, and a shim
//     rendering a prompt has nothing useful to do with an error there.
func (p *plugin) bridgeJSONEncode(L *lua.LState) int {
	var b strings.Builder
	encodeLua(&b, L.Get(1))
	L.Push(lua.LString(b.String()))
	return 1
}

func encodeLua(b *strings.Builder, v lua.LValue) {
	switch x := v.(type) {
	case lua.LString:
		encodeLuaString(b, string(x))
	case lua.LNumber:
		f := float64(x)
		if f == float64(int64(f)) {
			b.WriteString(strconv.FormatInt(int64(f), 10))
		} else {
			b.WriteString(strconv.FormatFloat(f, 'g', -1, 64))
		}
	case lua.LBool:
		if bool(x) {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case *lua.LTable:
		encodeLuaTable(b, x)
	default:
		b.WriteString("null")
	}
}

func encodeLuaTable(b *strings.Builder, t *lua.LTable) {
	n := 0
	t.ForEach(func(lua.LValue, lua.LValue) { n++ })
	if n == t.Len() {
		b.WriteByte('[')
		for i := 1; i <= t.Len(); i++ {
			if i > 1 {
				b.WriteByte(',')
			}
			encodeLua(b, t.RawGetInt(i))
		}
		b.WriteByte(']')
		return
	}
	keys := make([]string, 0, n)
	for _, k := range tableKeys(t) {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		encodeLuaString(b, k)
		b.WriteByte(':')
		encodeLua(b, t.RawGetString(k))
	}
	b.WriteByte('}')
}

// tableKeys collects the keys as strings. A non-string key is rendered the way Lua would print it,
// because that is what the copies did with `jstr(k)` — a table mixing key kinds is not a shape a
// shim produces, and inventing an error for it here would be a new behaviour rather than a move.
func tableKeys(t *lua.LTable) []string {
	var out []string
	t.ForEach(func(k, _ lua.LValue) {
		out = append(out, k.String())
	})
	return out
}

// encodeLuaString writes a JSON string. Escaping matches the copies exactly, control characters
// included: \u00XX for anything below 0x20 that has no shorter escape. Bytes above 0x7f pass
// through, because the copies passed them through and a prompt is UTF-8 already.
func encodeLuaString(b *strings.Builder, s string) {
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if c < 0x20 {
				b.WriteString(`\u00`)
				const hex = "0123456789abcdef"
				b.WriteByte(hex[c>>4])
				b.WriteByte(hex[c&0xf])
			} else {
				b.WriteByte(c)
			}
		}
	}
	b.WriteByte('"')
}
