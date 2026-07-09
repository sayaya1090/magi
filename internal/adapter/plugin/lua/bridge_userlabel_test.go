package lua

import (
	"strings"
	"testing"
)

type fakeUserReg struct{ got string }

func (f *fakeUserReg) SetUserLabel(label string) { f.got = label }

// A plugin that declares the "ui" capability can inject the transcript's user
// label; the value reaches the registry and set_user_label returns true.
func TestSetUserLabelGranted(t *testing.T) {
	reg := &fakeUserReg{}
	_, out := loadHost(t, HostConfig{UserReg: reg},
		`name="p"`+"\n"+`capabilities=["ui"]`,
		`local ok = magi.set_user_label("Alice")
magi.log("ok=" .. tostring(ok))`)
	if reg.got != "Alice" {
		t.Errorf("registry got %q, want the requested label", reg.got)
	}
	if !strings.Contains(out, "ok=true") {
		t.Errorf("set_user_label should return true: %q", out)
	}
}

// Without the "ui" capability the call is refused at the bridge (a raised Lua
// error) and the registry is never touched.
func TestSetUserLabelNeedsCap(t *testing.T) {
	reg := &fakeUserReg{}
	_, out := loadHost(t, HostConfig{UserReg: reg},
		`name="p"`+"\n"+`capabilities=["tool"]`,
		`local ok, e = pcall(function() magi.set_user_label("Alice") end)
magi.log("ok=" .. tostring(ok) .. " e=" .. tostring(e))`)
	if reg.got != "" {
		t.Errorf("ungranted set_user_label must not reach the registry, got %q", reg.got)
	}
	if !strings.Contains(out, "ok=false") || !strings.Contains(out, `capability "ui" not declared`) {
		t.Errorf("expected a capability-denied error: %q", out)
	}
}

// An empty (or whitespace-only) label is rejected before the registry is called.
func TestSetUserLabelRejectsEmpty(t *testing.T) {
	reg := &fakeUserReg{}
	_, out := loadHost(t, HostConfig{UserReg: reg},
		`name="p"`+"\n"+`capabilities=["ui"]`,
		`local ok, e = magi.set_user_label("   ")
magi.log("e=" .. tostring(e))`)
	if reg.got != "" {
		t.Errorf("empty label must not reach the registry, got %q", reg.got)
	}
	if !strings.Contains(out, "non-empty") {
		t.Errorf("expected a non-empty error: %q", out)
	}
}

// With no registry wired (headless / feature off), set_user_label reports
// unavailability rather than panicking.
func TestSetUserLabelUnavailable(t *testing.T) {
	_, out := loadHost(t, HostConfig{},
		`name="p"`+"\n"+`capabilities=["ui"]`,
		`local ok, e = magi.set_user_label("Alice")
magi.log("e=" .. tostring(e))`)
	if !strings.Contains(out, "not available") {
		t.Errorf("expected an unavailable error: %q", out)
	}
}
