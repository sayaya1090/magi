package app

import (
	"strings"
	"testing"
)

func TestUserCompanionPurposeReachesOwnSystemContext(t *testing.T) {
	a, _ := newApp(t, &fakeLLM{}, Config{CompanionInstructions: func(workdir string) string { return "Purpose: inspect GWT. Do not deploy. Workspace: " + workdir }})
	parts := a.systemFragments(AgentSpec{Name: "default"}, "/workspace/gwt")
	found := false
	for _, p := range parts {
		if p.id == "system/companion-purpose" {
			found = true
			if !strings.Contains(p.text, "Do not deploy") || !strings.Contains(p.text, "/workspace/gwt") {
				t.Fatal(p)
			}
		}
	}
	if !found {
		t.Fatal("user declaration absent from model context")
	}
}
