package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
