package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

func TestTokenize(t *testing.T) {
	got := tokenize("Use TABS, not 2 spaces! go_lang")
	// lowercased, split on non-alnum, words <3 chars dropped (so "go", "2", "not"? "not" is 3 → kept).
	for _, w := range []string{"use", "tabs", "spaces", "not"} {
		if !got[w] {
			t.Errorf("expected token %q in %v", w, got)
		}
	}
	for _, w := range []string{"go", "2"} { // too short
		if got[w] {
			t.Errorf("short token %q should be dropped", w)
		}
	}
	// "go_lang" splits on '_' into "go"(dropped) + "lang"(kept).
	if !got["lang"] || got["go_lang"] {
		t.Errorf("underscore should split: %v", got)
	}
}

func TestOverlap(t *testing.T) {
	terms := tokenize("tabs indentation")
	if n := overlap(terms, "Always use TABS for INDENTATION here"); n != 2 {
		t.Errorf("overlap = %d, want 2", n)
	}
	if n := overlap(terms, "nothing relevant"); n != 0 {
		t.Errorf("no-match overlap = %d, want 0", n)
	}
	if n := overlap(map[string]bool{}, "anything"); n != 0 {
		t.Errorf("empty terms should score 0, got %d", n)
	}
	// Substring match (not word-boundary): "port" is in "important".
	if n := overlap(tokenize("port"), "this is important"); n != 1 {
		t.Errorf("substring overlap = %d, want 1", n)
	}
}

// topMemories ranks by score desc, drops zero-score entries, caps at n, and is
// stable for ties (input order preserved).
func TestTopMemoriesRanking(t *testing.T) {
	in := []scored[port.Memory]{
		{score: 0, v: port.Memory{ID: "zero"}},
		{score: 2, v: port.Memory{ID: "a"}},
		{score: 5, v: port.Memory{ID: "b"}},
		{score: 2, v: port.Memory{ID: "c"}}, // tie with a → a before c (stable)
	}
	got := topMemories(in, 2)
	if len(got) != 2 || got[0].ID != "b" || got[1].ID != "a" {
		t.Fatalf("ranking/cap wrong: %+v", got)
	}
	// All-zero input → no results (nothing relevant).
	if z := topMemories([]scored[port.Memory]{{score: 0, v: port.Memory{ID: "x"}}}, 5); len(z) != 0 {
		t.Errorf("zero-score entries must be dropped, got %+v", z)
	}
}

func TestSplitFirstLine(t *testing.T) {
	desc, body := splitFirstLine("a short skill\nline two\nline three")
	if desc != "a short skill" || body != "line two\nline three" {
		t.Errorf("split = %q / %q", desc, body)
	}
	d2, b2 := splitFirstLine("only one line")
	if d2 != "only one line" || b2 != "" {
		t.Errorf("single line split = %q / %q", d2, b2)
	}
}

// sanitize must strip path separators and dots so a skill name can't escape the
// pending directory (path-traversal safety) — only [A-Za-z0-9-_] survive.
func TestSanitizePathSafety(t *testing.T) {
	got := sanitize("../../etc/pass wd.sh")
	if strings.ContainsAny(got, "/.") || strings.Contains(got, " ") {
		t.Errorf("sanitize left unsafe chars: %q", got)
	}
	if sanitize("ok-name_1") != "ok-name_1" {
		t.Errorf("safe chars should be preserved, got %q", sanitize("ok-name_1"))
	}
}

// Retrieve must also surface skills, splitting the first line into the description.
func TestRetrieveSkills(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "skills"), 0o755)
	os.WriteFile(filepath.Join(dir, "skills", "deploy.md"),
		[]byte("how to deploy the service\nstep 1\nstep 2"), 0o644)

	_, skills, err := New(dir).Retrieve(context.Background(), "how do I deploy")
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 1 {
		t.Fatalf("want 1 skill, got %d", len(skills))
	}
	if skills[0].Name != "deploy" || skills[0].Description != "how to deploy the service" || !strings.Contains(skills[0].Body, "step 1") {
		t.Errorf("skill parsed wrong: %+v", skills[0])
	}
}

// Propose writes each memory (with tags + source) and each skill (under a
// sanitized, non-escaping filename) into pending/.
func TestProposeMemoriesAndSkills(t *testing.T) {
	dir := t.TempDir()
	err := New(dir).Propose(context.Background(), port.Contribution{
		Memories: []port.Memory{
			{Text: "first memory", Tags: []string{"a", "b"}},
			{Text: "second memory"},
		},
		Skills: []port.Skill{{Name: "../evil name", Description: "d", Body: "b"}},
		Source: "agent",
	})
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "pending"))
	if len(entries) != 3 { // 2 memories + 1 skill
		t.Fatalf("want 3 pending files, got %d", len(entries))
	}
	var sawSkill, sawTagged bool
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "skill-") {
			sawSkill = true
			// The stem (filename minus the "skill-" prefix and ".md" suffix) must contain
			// no path separators or dots — i.e. the unsafe name was fully sanitized and
			// can't escape pending/.
			stem := strings.TrimSuffix(strings.TrimPrefix(name, "skill-"), ".md")
			if strings.ContainsAny(stem, "/.") {
				t.Errorf("skill filename not sanitized (escapable): %q", name)
			}
			if name != "skill-"+sanitize("../evil name")+".md" {
				t.Errorf("skill filename = %q, want sanitized form", name)
			}
		}
		b, _ := os.ReadFile(filepath.Join(dir, "pending", name))
		if strings.Contains(string(b), "tags: a, b") {
			sawTagged = true
		}
		if strings.HasPrefix(name, "mem-") && !strings.Contains(string(b), "(source: agent)") {
			t.Errorf("memory %q missing source attribution", name)
		}
	}
	if !sawSkill || !sawTagged {
		t.Errorf("expected a skill file and a tagged memory (skill=%v tagged=%v)", sawSkill, sawTagged)
	}
}
