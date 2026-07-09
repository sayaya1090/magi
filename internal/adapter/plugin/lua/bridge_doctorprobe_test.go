package lua

import (
	"context"
	"strings"
	"testing"
	"time"
)

// A plugin that declares the "doctor" capability registers environment probes;
// the host aggregates them and running one returns its (status, detail).
func TestRegisterDoctorProbesGranted(t *testing.T) {
	h, _ := loadHost(t, HostConfig{},
		`name="p"`+"\n"+`capabilities=["doctor"]`,
		`magi.register_doctor_probes{
  token = function() return "ok", "cached token valid" end,
}`)
	probes := h.DoctorProbes()
	if len(probes) != 1 {
		t.Fatalf("want 1 probe, got %d", len(probes))
	}
	if probes[0].Name() != "token" {
		t.Errorf("probe name = %q, want token", probes[0].Name())
	}
	status, detail := probes[0].Run(context.Background())
	if status != "ok" || detail != "cached token valid" {
		t.Errorf("Run = (%q,%q), want (ok, cached token valid)", status, detail)
	}
}

// Without the "doctor" capability the registration is refused at the bridge and
// the host collects no probes.
func TestRegisterDoctorProbesNeedsCap(t *testing.T) {
	h, out := loadHost(t, HostConfig{},
		`name="p"`+"\n"+`capabilities=["tool"]`,
		`local ok, e = pcall(function()
  magi.register_doctor_probes{ x = function() return "ok","" end }
end)
magi.log("ok=" .. tostring(ok) .. " e=" .. tostring(e))`)
	if len(h.DoctorProbes()) != 0 {
		t.Errorf("ungranted registration must not collect probes")
	}
	if !strings.Contains(out, "ok=false") || !strings.Contains(out, `capability "doctor" not declared`) {
		t.Errorf("expected a capability-denied error: %q", out)
	}
}

// A probe that returns a non-string status is coerced to "info" by the adapter,
// so the doctor report never sees an unknown value.
func TestDoctorProbeNilStatusIsInfo(t *testing.T) {
	h, _ := loadHost(t, HostConfig{},
		`name="p"`+"\n"+`capabilities=["doctor"]`,
		`magi.register_doctor_probes{ quiet = function() return nil, "no status" end }`)
	probes := h.DoctorProbes()
	if len(probes) != 1 {
		t.Fatalf("want 1 probe, got %d", len(probes))
	}
	status, detail := probes[0].Run(context.Background())
	if status != "info" {
		t.Errorf("nil status should coerce to info, got %q", status)
	}
	if detail != "no status" {
		t.Errorf("detail = %q", detail)
	}
}

// A probe whose Lua function errors surfaces as a "fail" so a broken check is
// visible in the report rather than silently dropped.
func TestDoctorProbeErrorIsFail(t *testing.T) {
	h, _ := loadHost(t, HostConfig{},
		`name="p"`+"\n"+`capabilities=["doctor"]`,
		`magi.register_doctor_probes{ boom = function() error("kaboom") end }`)
	status, detail := h.DoctorProbes()[0].Run(context.Background())
	if status != "fail" || !strings.Contains(detail, "kaboom") {
		t.Errorf("Run = (%q,%q), want a fail carrying the error", status, detail)
	}
}

// Multiple probes share one plugin LState and run sequentially; an errored probe
// must not corrupt the Lua stack for a later one (CallByParam Protect resets it).
func TestDoctorProbesSequentialStackClean(t *testing.T) {
	h, _ := loadHost(t, HostConfig{},
		`name="p"`+"\n"+`capabilities=["doctor"]`,
		`magi.register_doctor_probes{
  boom = function() error("kaboom") end,
  good = function() return "ok", "still fine" end,
}`)
	got := map[string][2]string{}
	for _, p := range h.DoctorProbes() {
		s, d := p.Run(context.Background())
		got[p.Name()] = [2]string{s, d}
	}
	if got["boom"][0] != "fail" {
		t.Errorf("boom = %v, want fail", got["boom"])
	}
	if got["good"][0] != "ok" || got["good"][1] != "still fine" {
		t.Errorf("good after an errored probe = %v, want (ok, still fine)", got["good"])
	}
}

// A runaway probe (infinite Lua loop) is bounded by the run context: SetContext
// makes the VM abort so -doctor cannot hang on a misbehaving check.
func TestDoctorProbeContextBounded(t *testing.T) {
	h, _ := loadHost(t, HostConfig{},
		`name="p"`+"\n"+`capabilities=["doctor"]`,
		`magi.register_doctor_probes{ spin = function() while true do end end }`)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	var status string
	go func() {
		status, _ = h.DoctorProbes()[0].Run(ctx)
		close(done)
	}()
	select {
	case <-done:
		if status != "fail" {
			t.Errorf("a cancelled runaway probe should fail, got %q", status)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("probe was not bounded by the context (still running after 5s)")
	}
}

// An empty table (no name=function pairs) is rejected so a typo can't silently
// register nothing.
func TestRegisterDoctorProbesEmpty(t *testing.T) {
	h, out := loadHost(t, HostConfig{},
		`name="p"`+"\n"+`capabilities=["doctor"]`,
		`local ok, e = pcall(function() magi.register_doctor_probes{} end)
magi.log("ok=" .. tostring(ok) .. " e=" .. tostring(e))`)
	if len(h.DoctorProbes()) != 0 {
		t.Errorf("empty registration should collect no probes")
	}
	if !strings.Contains(out, "ok=false") || !strings.Contains(out, "expected a table") {
		t.Errorf("expected an empty-table error: %q", out)
	}
}
