package lua

import (
	"errors"
	"strings"
	"testing"
)

type fakeModelReg struct {
	got       string // last model passed to SetModel
	gotWinID  string // last model passed to SetContextWindow
	gotWin    int    // last tokens passed to SetContextWindow
	winCalled bool
	err       error
}

func (f *fakeModelReg) SetModel(m string) error {
	if f.err != nil {
		return f.err
	}
	f.got = m
	return nil
}

func (f *fakeModelReg) SetContextWindow(modelID string, tokens int) error {
	if f.err != nil {
		return f.err
	}
	f.gotWinID = modelID
	f.gotWin = tokens
	f.winCalled = true
	return nil
}

// A granted plugin steers the session model; the value reaches the registry and
// set_model returns true.
func TestSetModelGranted(t *testing.T) {
	reg := &fakeModelReg{}
	_, out := loadHost(t, HostConfig{ModelReg: reg},
		`name="p"`+"\n"+`permissions=["config:write:model"]`,
		`local ok = magi.set_model("gpt-oss:120b-cloud")
magi.log("ok=" .. tostring(ok))`)
	if reg.got != "gpt-oss:120b-cloud" {
		t.Errorf("registry got %q, want the requested model", reg.got)
	}
	if !strings.Contains(out, "ok=true") {
		t.Errorf("set_model should return true: %q", out)
	}
}

// Without the config:write:model grant, set_model is refused and the registry is
// never touched.
func TestSetModelNeedsGrant(t *testing.T) {
	reg := &fakeModelReg{}
	_, out := loadHost(t, HostConfig{ModelReg: reg},
		`name="p"`+"\n"+`capabilities=["tool"]`,
		`local ok, e = magi.set_model("x")
magi.log("e=" .. tostring(e))`)
	if reg.got != "" {
		t.Errorf("ungranted set_model must not reach the registry, got %q", reg.got)
	}
	if !strings.Contains(out, "permission denied: config:write:model") {
		t.Errorf("expected a permission-denied error: %q", out)
	}
}

// An empty model id is rejected before the registry is called.
func TestSetModelRejectsEmpty(t *testing.T) {
	reg := &fakeModelReg{}
	_, out := loadHost(t, HostConfig{ModelReg: reg},
		`name="p"`+"\n"+`permissions=["config:write:model"]`,
		`local ok, e = magi.set_model("   ")
magi.log("e=" .. tostring(e))`)
	if reg.got != "" {
		t.Errorf("empty model must not reach the registry, got %q", reg.got)
	}
	if !strings.Contains(out, "non-empty") {
		t.Errorf("expected a non-empty error: %q", out)
	}
}

// With no registry wired (e.g. headless with the feature disabled), set_model
// reports unavailability rather than panicking.
func TestSetModelUnavailable(t *testing.T) {
	_, out := loadHost(t, HostConfig{},
		`name="p"`+"\n"+`permissions=["config:write:model"]`,
		`local ok, e = magi.set_model("x")
magi.log("e=" .. tostring(e))`)
	if !strings.Contains(out, "not available") {
		t.Errorf("expected an unavailable error: %q", out)
	}
}

// A registry failure surfaces to the plugin as (nil, err).
func TestSetModelRegistryError(t *testing.T) {
	reg := &fakeModelReg{err: errors.New("backend down")}
	_, out := loadHost(t, HostConfig{ModelReg: reg},
		`name="p"`+"\n"+`permissions=["config:write:model"]`,
		`local ok, e = magi.set_model("x")
magi.log("e=" .. tostring(e))`)
	if !strings.Contains(out, "backend down") {
		t.Errorf("registry error should surface: %q", out)
	}
}
