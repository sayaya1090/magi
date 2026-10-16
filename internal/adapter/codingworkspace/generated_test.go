package codingworkspace

import (
	"context"
	task "github.com/sayaya1090/magi/internal/core/codingtask"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func nested(t *testing.T, root, path, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(path))
	if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
		t.Fatal(e)
	}
	put(t, p, content)
}
func TestGradleOutputsDoNotBreakPrepareOrPostBuildDiff(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	m := New(t.TempDir())
	m.MaxBytes = 256
	put(t, filepath.Join(source, "build.gradle.kts"), "plugins {}\n")
	put(t, filepath.Join(source, "source.kt"), "before\n")
	nested(t, source, "build/tmp/test/work/.gradle-test-kit/caches/gradle-api.jar", strings.Repeat("x", 1024))
	nested(t, source, ".gradle/caches/cache.bin", strings.Repeat("x", 1024))
	nested(t, source, "test/build.gradle.kts", "plugins {}\n")
	nested(t, source, "test/build/output.jar", strings.Repeat("x", 1024))
	r, e := m.Prepare(ctx, source, task.Request{Mode: "copy"})
	if e != nil {
		t.Fatal(e)
	}
	for _, path := range []string{"build", ".gradle", "test/build"} {
		if _, e := os.Stat(filepath.Join(r.Workspace, filepath.FromSlash(path))); !os.IsNotExist(e) {
			t.Fatalf("generated directory copied: %s", path)
		}
	}
	if len(r.Excluded) != 3 {
		t.Fatalf("excluded paths not disclosed: %v", r.Excluded)
	}
	nested(t, r.Workspace, "build/tmp/test/work/.gradle-test-kit/caches/gradle-api.jar", strings.Repeat("x", 2048))
	nested(t, r.Workspace, ".gradle/caches/cache.bin", strings.Repeat("x", 2048))
	put(t, filepath.Join(r.Workspace, "source.kt"), "after\n")
	diff, e := m.Diff(ctx, source, r.ID)
	if e != nil || len(diff) != 1 || diff[0].Path != "source.kt" {
		t.Fatalf("diff=%+v error=%v", diff, e)
	}
}
func TestBuildNamedSourceAndPriorBaselineAreNotDropped(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	m := New(t.TempDir())
	nested(t, source, "build/source.txt", "before")
	nested(t, source, "src/build/source.txt", "essential")
	r, e := m.Prepare(ctx, source, task.Request{Mode: "copy"})
	if e != nil {
		t.Fatal(e)
	}
	// A build marker added later must not turn old managed files into deletions.
	put(t, filepath.Join(r.Workspace, "build.gradle.kts"), "plugins {}")
	nested(t, r.Workspace, "build/source.txt", "after")
	diff, e := m.Diff(ctx, source, r.ID)
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, d := range diff {
		if d.Kind == "delete" {
			t.Fatalf("baseline scope lost: %+v", d)
		}
		if d.Path == "build/source.txt" {
			found = d.Kind == "modify"
		}
	}
	if !found {
		t.Fatalf("old baseline change hidden: %+v", diff)
	}
	if contents(t, filepath.Join(r.Workspace, "src/build/source.txt")) != "essential" {
		t.Fatal("source named build was excluded")
	}
}
func TestTrackedBuildDirectoryIsPreserved(t *testing.T) {
	if _, e := exec.LookPath("git"); e != nil {
		t.Skip("Git not installed")
	}
	ctx := context.Background()
	source := t.TempDir()
	for _, args := range [][]string{{"init"}, {"config", "user.email", "test@example.invalid"}, {"config", "user.name", "test"}} {
		if _, e := git(ctx, source, args...); e != nil {
			t.Fatal(e)
		}
	}
	put(t, filepath.Join(source, "build.gradle.kts"), "plugins {}")
	nested(t, source, "build/required-source.txt", "tracked source")
	if _, e := git(ctx, source, "add", "."); e != nil {
		t.Fatal(e)
	}
	if _, e := git(ctx, source, "commit", "-m", "source"); e != nil {
		t.Fatal(e)
	}
	m := New(t.TempDir())
	r, e := m.Prepare(ctx, source, task.Request{})
	if e != nil {
		t.Fatal(e)
	}
	if _, ok := r.Baseline["build/required-source.txt"]; !ok {
		t.Fatal("tracked build source omitted")
	}
	nested(t, r.Workspace, "build/required-source.txt", "modified source")
	diff, e := m.Diff(ctx, source, r.ID)
	if e != nil || len(diff) != 1 || diff[0].Kind != "modify" {
		t.Fatalf("%+v %v", diff, e)
	}
}
