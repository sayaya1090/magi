package lua

import (
	"context"

	lua "github.com/yuin/gopher-lua"
)

// luaDoctorProbe adapts a Lua function to port.DoctorProbe: a plugin-contributed
// check that `magi -doctor` runs and folds into its report. The Lua function takes
// no arguments and returns (status, detail) — status is one of ok|warn|fail|info.
type luaDoctorProbe struct {
	plugin *plugin
	name   string
	fn     *lua.LFunction
}

func (d *luaDoctorProbe) Name() string { return d.name }

// Run invokes the probe's Lua function under the plugin lock (the LState is not
// concurrency-safe) and returns its (status, detail). A Lua error or an unloaded
// plugin surfaces as a "fail" so a broken probe is visible rather than silent. The
// ctx bounds the call: SetContext makes the gopher-lua VM abort at the next opcode
// once ctx is cancelled, so a runaway probe (e.g. a bare `while true`) fails within
// the doctor command's timeout instead of hanging the diagnostic. A probe that
// blocks inside a Go bridge call relies on that call's own deadline.
func (d *luaDoctorProbe) Run(ctx context.Context) (status, detail string) {
	d.plugin.mu.Lock()
	defer d.plugin.mu.Unlock()

	L := d.plugin.L
	if L == nil {
		return "fail", "plugin unloaded"
	}
	L.SetContext(ctx)
	defer L.RemoveContext()
	if err := L.CallByParam(lua.P{Fn: d.fn, NRet: 2, Protect: true}); err != nil {
		return "fail", "probe error: " + err.Error()
	}
	// Returns are pushed in order; pop detail (top) then status.
	dv := L.Get(-1)
	sv := L.Get(-2)
	L.Pop(2)
	status = statusStr(sv)
	if s, ok := dv.(lua.LString); ok {
		detail = string(s)
	}
	return status, detail
}

// statusStr coerces a Lua return to a status string, defaulting to "info" when the
// probe returned nil/non-string (the doctor command re-clamps unknown values too).
func statusStr(v lua.LValue) string {
	if s, ok := v.(lua.LString); ok {
		return string(s)
	}
	return "info"
}
