package app

import (
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// The push side is a one-line count pointer — it must NOT contain the entries' text,
// so relevant knowledge stays reachable without spending context every turn.
func TestExperiencePointerIsCountOnly(t *testing.T) {
	if p := experiencePointer(0, 0); p != "" {
		t.Fatalf("no matches should drop the section, got %q", p)
	}
	p := experiencePointer(2, 1)
	if !strings.Contains(p, "3 relevant") {
		t.Errorf("pointer should report the total (3), got %q", p)
	}
	if !strings.Contains(p, "recall_memory") {
		t.Errorf("pointer should route the agent to recall_memory, got %q", p)
	}
	// Singular vs plural.
	if !strings.Contains(experiencePointer(1, 0), "entry ") {
		t.Errorf("one match should read 'entry', got %q", experiencePointer(1, 0))
	}
	if !strings.Contains(experiencePointer(2, 0), "entries ") {
		t.Errorf("two matches should read 'entries', got %q", experiencePointer(2, 0))
	}
}

// The pull side (recall_memory) is where the actual entry text enters context.
func TestFormatExperienceFullRendersDetail(t *testing.T) {
	out := formatExperienceFull(
		[]port.Memory{{Text: "[project] always run gofmt before commit"}},
		[]port.Skill{{Name: "[global] deploy", Description: "ship to staging first", Body: "step1\nstep2"}},
	)
	for _, want := range []string{"always run gofmt", "skill [global] deploy", "ship to staging first", "step1", "step2"} {
		if !strings.Contains(out, want) {
			t.Errorf("pull output missing %q; got:\n%s", want, out)
		}
	}
}
