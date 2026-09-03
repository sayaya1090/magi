package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
)

func TestLoadSkills(t *testing.T) {
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := New(store, nil, builtin.Default(), bus.New(), nil, Config{}) // nil platform → only workdir/.magi/skills
	wd := t.TempDir()
	skdir := filepath.Join(wd, ".magi", "skills")
	if err := os.MkdirAll(skdir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(skdir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("deploy.md", "Deploy the app to prod\n\nstep 1\nstep 2")
	write("empty.md", "   ")          // blank → skipped
	write("notes.txt", "not a skill") // non-.md → skipped

	sk := a.loadSkills(wd)
	if len(sk) != 1 {
		t.Fatalf("expected 1 skill (blank/non-md skipped), got %d: %+v", len(sk), sk)
	}
	if sk[0].Name != "deploy" || sk[0].Description != "Deploy the app to prod" {
		t.Errorf("skill = %+v (name/first-line description)", sk[0])
	}
	body, ok := a.skillBody(wd, "deploy")
	if !ok || !strings.Contains(body, "step 2") {
		t.Errorf("skillBody = %q, ok=%v (should be the full body)", body, ok)
	}
	if _, ok := a.skillBody(wd, "nope"); ok {
		t.Error("an unknown skill must not be found")
	}
}

// A skill written the usual way — with frontmatter — describes itself, not "---".
//
// The same function already parses frontmatter for the directory form, and read only the first line
// for the flat one. So a file written the way every skill in every other tool is written advertised
// itself as three dashes: in the model's prompt, in `about`, and across the network into what other
// machines believe this companion can do. Seen live on five companions, every one of them.
func TestAFlatSkillWithFrontmatterDescribesItself(t *testing.T) {
	wd := t.TempDir()
	dir := filepath.Join(wd, ".magi", "skills")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: bisect\ndescription: walks history to the commit that broke it\n---\n\nSteps.\n"
	if err := os.WriteFile(filepath.Join(dir, "bisect.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	a := New(nil, nil, nil, nil, nil, Config{})
	got := a.Skills(wd)
	if len(got) != 1 {
		t.Fatalf("%d skills found", len(got))
	}
	if got[0].Description != "walks history to the commit that broke it" {
		t.Fatalf("it describes itself as %q", got[0].Description)
	}
}

// A plain file with no frontmatter still uses its first line.
func TestAFlatSkillWithoutFrontmatterStillUsesItsFirstLine(t *testing.T) {
	wd := t.TempDir()
	dir := filepath.Join(wd, ".magi", "skills")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tidy.md"), []byte("tidies the tree\n\nSteps.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := New(nil, nil, nil, nil, nil, Config{}).Skills(wd)
	if len(got) != 1 || got[0].Description != "tidies the tree" {
		t.Fatalf("got %+v", got)
	}
}

// A flat skill edited in place is served fresh. The signature stat'd directories and SKILL.md
// files but never the flat .md entries themselves, so an in-place overwrite changed nothing it
// measured and the daemon served the old body until restart.
func TestAFlatSkillEditedInPlaceIsServedFresh(t *testing.T) {
	wd := t.TempDir()
	a := &App{}
	dir := filepath.Join(wd, ".magi", "skills")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "deploy.md"), []byte("how to deploy\n\nrun make ship\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	body, ok := a.skillBody(wd, "deploy")
	if !ok || !strings.Contains(body, "make ship") {
		t.Fatalf("the flat skill did not load: ok=%v body=%q", ok, body)
	}
	time.Sleep(10 * time.Millisecond) // a distinct mtime on coarse filesystems
	if err := os.WriteFile(filepath.Join(dir, "deploy.md"), []byte("how to deploy\n\nrun make deploy-v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	body2, _ := a.skillBody(wd, "deploy")
	if !strings.Contains(body2, "deploy-v2") {
		t.Fatalf("the edited skill is still served stale: %q", body2)
	}
}

// TestASubdirectoryOfSkillsIsInert pins what the PowerPoint task pane's guide manager stands on:
// a skill moved into a SUBDIRECTORY of .magi/skills is not loaded.
//
// That client offers "disable" next to "delete", and the difference between them is the whole
// point — a disabled guide keeps its text so it can come back. It implements that by moving the
// file into .magi/skills/off/, which is inert only because the loader skips directories.
//
// This test is here rather than in the client because a test written over there would only pin
// the client's UNDERSTANDING of this loader. If the loader ever descends into subdirectories,
// every guide a person switched off comes back on, silently, in their next turn.
func TestASubdirectoryOfSkillsIsInert(t *testing.T) {
	wd := t.TempDir()
	a := &App{}
	dir := filepath.Join(wd, ".magi", "skills")
	off := filepath.Join(dir, "off")
	if err := os.MkdirAll(off, 0o755); err != nil {
		t.Fatal(err)
	}
	on := "---\ndescription: on\n---\nbody"
	if err := os.WriteFile(filepath.Join(dir, "kept.md"), []byte(on), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(off, "hidden.md"), []byte("---\ndescription: off\n---\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	skills := a.loadSkills(wd)
	if len(skills) != 1 || skills[0].Name != "kept" {
		t.Fatalf("only the enabled guide should load, got %+v", skills)
	}
	// And the description is the one the manager shows: frontmatter, not the first line.
	if skills[0].Description != "on" {
		t.Errorf("description should come from frontmatter, got %q", skills[0].Description)
	}
}
