package app

import (
	"github.com/sayaya1090/magi/internal/core/session"
	"reflect"
	"testing"
)

func TestCouncilSwitchKeepsCurrentTurnAndIsolatesSessions(t *testing.T) {
	a, _ := newApp(t, &fakeLLM{}, Config{Council: &fakeCouncil{}})
	sid := session.SessionID("one")
	other := session.SessionID("two")
	a.snapshotCouncil(sid)
	wd := t.TempDir()
	toolsBefore := a.toolSpecs(sid, AgentSpec{})
	systemBefore := a.buildStepSystem(sid, AgentSpec{}, wd, nil, nil)
	if err := a.SetCouncil(sid, false); err != nil {
		t.Fatal(err)
	}
	if a.councilFor(sid) == nil {
		t.Fatal("running turn changed")
	}
	a.snapshotCouncil(sid)
	if a.councilFor(sid) != nil {
		t.Fatal("next turn did not switch off")
	}
	if !reflect.DeepEqual(toolsBefore, a.toolSpecs(sid, AgentSpec{})) {
		t.Fatal("toggle changed cacheable tool schemas")
	}
	if systemBefore != a.buildStepSystem(sid, AgentSpec{}, wd, nil, nil) {
		t.Fatal("toggle changed system prefix")
	}
	if a.councilFor(other) == nil {
		t.Fatal("another session changed")
	}
	if err := a.SetCouncil(sid, true); err != nil {
		t.Fatal(err)
	}
	if a.councilFor(sid) != nil {
		t.Fatal("disabled turn changed mid-flight")
	}
	a.snapshotCouncil(sid)
	if a.councilFor(sid) == nil {
		t.Fatal("next turn did not switch on")
	}
}

func TestInitiallyDisabledCouncilCanBeEnabledWithoutRestart(t *testing.T) {
	off := false
	a, _ := newApp(t, &fakeLLM{}, Config{Council: &fakeCouncil{}, CouncilInitiallyEnabled: &off})
	sid := session.SessionID("one")
	if a.councilFor(sid) != nil {
		t.Fatal("initially enabled")
	}
	if err := a.SetCouncil(sid, true); err != nil {
		t.Fatal(err)
	}
	a.snapshotCouncil(sid)
	if a.councilFor(sid) == nil {
		t.Fatal("reviewer not available")
	}
}
