package lua

import (
	"errors"
	"strings"
	"testing"
)

// A granted plugin overrides a model's context window; the tokens and (empty →
// session) model reach the registry and set_context_window returns true.
func TestSetContextWindowGranted(t *testing.T) {
	reg := &fakeModelReg{}
	_, out := loadHost(t, HostConfig{ModelReg: reg},
		`name="p"`+"\n"+`permissions=["config:write:model"]`,
		`local ok = magi.set_context_window(131072)
magi.log("ok=" .. tostring(ok))`)
	if !reg.winCalled || reg.gotWin != 131072 {
		t.Errorf("registry got window %d (called=%v), want 131072", reg.gotWin, reg.winCalled)
	}
	if reg.gotWinID != "" {
		t.Errorf("omitted model should target the session model (empty id), got %q", reg.gotWinID)
	}
	if !strings.Contains(out, "ok=true") {
		t.Errorf("set_context_window should return true: %q", out)
	}
}

// The optional second arg names a specific model instead of the session model.
func TestSetContextWindowNamedModel(t *testing.T) {
	reg := &fakeModelReg{}
	_, _ = loadHost(t, HostConfig{ModelReg: reg},
		`name="p"`+"\n"+`permissions=["config:write:model"]`,
		`magi.set_context_window(40000, "internal-llm")`)
	if reg.gotWinID != "internal-llm" || reg.gotWin != 40000 {
		t.Errorf("registry got (%q,%d), want (internal-llm,40000)", reg.gotWinID, reg.gotWin)
	}
}

// Without the config:write:model grant, set_context_window is refused and the
// registry is never touched.
func TestSetContextWindowNeedsGrant(t *testing.T) {
	reg := &fakeModelReg{}
	_, out := loadHost(t, HostConfig{ModelReg: reg},
		`name="p"`+"\n"+`capabilities=["tool"]`,
		`local ok, e = magi.set_context_window(1000)
magi.log("e=" .. tostring(e))`)
	if reg.winCalled {
		t.Error("ungranted set_context_window must not reach the registry")
	}
	if !strings.Contains(out, "permission denied: config:write:model") {
		t.Errorf("expected a permission-denied error: %q", out)
	}
}

// With no registry wired, set_context_window reports unavailability rather than
// panicking.
func TestSetContextWindowUnavailable(t *testing.T) {
	_, out := loadHost(t, HostConfig{},
		`name="p"`+"\n"+`permissions=["config:write:model"]`,
		`local ok, e = magi.set_context_window(1000)
magi.log("e=" .. tostring(e))`)
	if !strings.Contains(out, "not available") {
		t.Errorf("expected an unavailable error: %q", out)
	}
}

// A registry failure surfaces to the plugin as (nil, err).
func TestSetContextWindowRegistryError(t *testing.T) {
	reg := &fakeModelReg{err: errors.New("backend down")}
	_, out := loadHost(t, HostConfig{ModelReg: reg},
		`name="p"`+"\n"+`permissions=["config:write:model"]`,
		`local ok, e = magi.set_context_window(1000)
magi.log("e=" .. tostring(e))`)
	if !strings.Contains(out, "backend down") {
		t.Errorf("registry error should surface: %q", out)
	}
}
