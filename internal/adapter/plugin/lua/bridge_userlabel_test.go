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

// A UTF-8 (Korean) label passed from Lua reaches the registry as the exact same
// bytes — Lua strings are byte strings, so the gopher-lua bridge must neither
// re-encode nor escape them. This closes the Lua→Go leg of the label's UTF-8
// contract (the app→event→TUI legs are covered in internal/app and
// internal/adapter/tui); together they prove any escaped display comes from a
// plugin escaping the string itself, never from magi's pipeline.
func TestSetUserLabelUnicode(t *testing.T) {
	reg := &fakeUserReg{}
	const want = "변냁재" // U+BCC0 U+B0C1 U+C7AC
	_, _ = loadHost(t, HostConfig{UserReg: reg},
		`name="p"`+"\n"+`capabilities=["ui"]`,
		`magi.set_user_label("`+want+`")`)
	if reg.got != want {
		t.Fatalf("registry got %q, want %q — the Lua→Go bridge must preserve raw UTF-8", reg.got, want)
	}
	if strings.Contains(reg.got, `\u`) {
		t.Fatalf("label reached the registry ASCII-escaped: %q", reg.got)
	}
	if r := []rune(reg.got); len(r) != 3 || r[0] != 0xBCC0 || r[1] != 0xB0C1 || r[2] != 0xC7AC {
		t.Fatalf("label decoded to %d runes %U, want U+BCC0 U+B0C1 U+C7AC", len(r), r)
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
