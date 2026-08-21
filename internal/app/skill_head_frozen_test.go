package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
)

// A skill list rides at the HEAD of every request. The directory it comes from changes mid-session
// from several directions — engram saving what it just learned, the agent writing a skill because
// the user asked for one, a person dropping a file in — and re-rendering the head for any of them
// moves position 0, which ends the cached prefix and re-bills the whole conversation.
//
// So the head is frozen for the life of the session and later arrivals are announced instead.
// These pin both halves.

func writeSkill(t *testing.T, dir, name, desc string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(desc+"\nbody\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSkillHeadIsFrozenForTheSession(t *testing.T) {
	work := t.TempDir()
	sk := filepath.Join(work, ".magi", "skills")
	writeSkill(t, sk, "alpha", "does the first thing")

	a := &App{states: map[session.SessionID]*sessionState{}}
	first := a.skillBlockFor("s1", work)
	if !strings.Contains(first, "alpha") {
		t.Fatalf("the opening head should name the skill that existed, got %q", first)
	}

	writeSkill(t, sk, "beta", "does a second thing")
	again := a.skillBlockFor("s1", work)
	if again != first {
		t.Fatalf("the head moved when a skill was added — that is the whole prefix gone:\n  was: %q\n  now: %q", first, again)
	}
	if strings.Contains(again, "beta") {
		t.Fatal("the new skill was folded into the head instead of being appended")
	}
}

func TestSkillArrivalsAreAnnouncedOnce(t *testing.T) {
	work := t.TempDir()
	sk := filepath.Join(work, ".magi", "skills")
	writeSkill(t, sk, "alpha", "does the first thing")

	a := &App{states: map[session.SessionID]*sessionState{}}
	if got := a.skillArrivals("s1", work); got != nil {
		t.Fatalf("before the head is written there is nothing to announce — it carries them: %v", got)
	}
	_ = a.skillBlockFor("s1", work)
	if got := a.skillArrivals("s1", work); got != nil {
		t.Fatalf("a skill already in the head must not be announced again: %v", got)
	}

	writeSkill(t, sk, "beta", "does a second thing")
	got := a.skillArrivals("s1", work)
	if len(got) != 1 || !strings.Contains(got[0], "beta") {
		t.Fatalf("the new skill should be announced exactly once, got %v", got)
	}
	if again := a.skillArrivals("s1", work); again != nil {
		t.Fatalf("announced twice, so the model reads it twice and the transcript grows for nothing: %v", again)
	}
}

// Each session opens its own head, so a skill announced in one is simply part of the next one's
// list — which is how an append-only head stays short instead of accumulating notes forever.
func TestANewSessionOpensWithTheSkillsThatExistNow(t *testing.T) {
	work := t.TempDir()
	sk := filepath.Join(work, ".magi", "skills")
	writeSkill(t, sk, "alpha", "does the first thing")

	a := &App{states: map[session.SessionID]*sessionState{}}
	_ = a.skillBlockFor("s1", work)
	writeSkill(t, sk, "beta", "does a second thing")
	_ = a.skillArrivals("s1", work)

	fresh := a.skillBlockFor("s2", work)
	if !strings.Contains(fresh, "alpha") || !strings.Contains(fresh, "beta") {
		t.Fatalf("a new session's head should carry both skills, got %q", fresh)
	}
	if got := a.skillArrivals("s2", work); got != nil {
		t.Fatalf("nothing is new to a session that just listed them: %v", got)
	}
}
