package app

import (
	"strings"
	"testing"
)

const sampleDiff = `diff --git a/main.go b/main.go
index 111..222 100644
--- a/main.go
+++ b/main.go
@@ -1,2 +1,2 @@
-old
+new
diff --git a/logo.png b/logo.png
new file mode 100644
index 0000000..333
Binary files /dev/null and b/logo.png differ
diff --git a/util.go b/util.go
index 444..555 100644
--- a/util.go
+++ b/util.go
@@ -1 +1 @@
-a
+b`

func TestSplitDiffFiles(t *testing.T) {
	files := splitDiffFiles(sampleDiff)
	if len(files) != 3 {
		t.Fatalf("want 3 file sections, got %d", len(files))
	}
	wantPaths := []string{"main.go", "logo.png", "util.go"}
	for i, w := range wantPaths {
		if files[i].path != w {
			t.Errorf("section %d path = %q, want %q", i, files[i].path, w)
		}
	}
	if !files[1].binary {
		t.Error("logo.png section should be detected as binary")
	}
	if files[0].binary || files[2].binary {
		t.Error("text sections must not be flagged binary")
	}
}

// filterCouncilDiff removes binary sections but keeps the text diffs intact.
// scopeDiffToTurn drops a file that was already dirty before the turn and wasn't touched,
// keeps a pre-dirty file the agent DID touch, and keeps brand-new files.
func TestScopeDiffToTurn(t *testing.T) {
	// main.go: pre-dirty + touched → keep. util.go: pre-dirty + untouched → drop.
	// (logo.png stays; it's not in dirtyBefore so it's a fresh change.)
	dirtyBefore := map[string]bool{"main.go": true, "util.go": true}
	touched := map[string]bool{"main.go": true}
	got := scopeDiffToTurn(sampleDiff, dirtyBefore, touched, "/repo")
	if !strings.Contains(got, "main.go") {
		t.Error("a pre-dirty file the agent TOUCHED should be kept")
	}
	if strings.Contains(got, "util.go") {
		t.Errorf("a pre-dirty UNtouched file should be dropped:\n%s", got)
	}
	if !strings.Contains(got, "logo.png") {
		t.Error("a file not dirty before the turn (fresh change) should be kept")
	}
	// No baseline → keep everything (prior behavior).
	if scopeDiffToTurn(sampleDiff, nil, nil, "/repo") != sampleDiff {
		t.Error("nil baseline should pass the diff through unchanged")
	}
	// The model may pass an ABSOLUTE (or "./") touched path while git paths are repo-relative;
	// it must still be normalized and matched, so the touched pre-dirty file is kept.
	gotAbs := scopeDiffToTurn(sampleDiff, dirtyBefore, map[string]bool{"/repo/main.go": true}, "/repo")
	if !strings.Contains(gotAbs, "main.go") {
		t.Error("an absolute touched path should normalize to repo-relative and keep the file")
	}
	if !strings.Contains(scopeDiffToTurn(sampleDiff, dirtyBefore, map[string]bool{"./main.go": true}, "/repo"), "main.go") {
		t.Error("a ./-prefixed touched path should normalize and keep the file")
	}
}

func TestFilterCouncilDiff(t *testing.T) {
	got := filterCouncilDiff(sampleDiff)
	if strings.Contains(got, "logo.png") || strings.Contains(got, "Binary files") {
		t.Errorf("binary section should be dropped:\n%s", got)
	}
	for _, want := range []string{"main.go", "util.go", "+new", "+b"} {
		if !strings.Contains(got, want) {
			t.Errorf("text diff content %q should be kept:\n%s", want, got)
		}
	}
	// An all-binary diff filters to empty (→ the turn reads as no code changes).
	binOnly := "diff --git a/x.pyc b/x.pyc\nBinary files a/x.pyc and b/x.pyc differ"
	if strings.TrimSpace(filterCouncilDiff(binOnly)) != "" {
		t.Errorf("all-binary diff should filter to empty, got %q", filterCouncilDiff(binOnly))
	}
	// A non-git-diff fallback (status --short) with no headers passes through.
	status := " M file.go\n?? new.txt"
	if filterCouncilDiff(status) != status {
		t.Errorf("header-less diff should pass through unchanged, got %q", filterCouncilDiff(status))
	}
}
