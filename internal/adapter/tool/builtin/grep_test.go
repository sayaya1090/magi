package builtin

import (
	"path/filepath"
	"testing"
)

// Models pass Claude-style multi-globs ("**/*.go,**/*.py"); the filter must
// honor comma lists and ** instead of silently matching nothing.
func TestGrepGlobMatch(t *testing.T) {
	cases := []struct {
		globs, rel, base string
		want             bool
	}{
		{"**/*.go,**/*.py", "a.go", "a.go", true},
		{"**/*.go,**/*.py", "pkg/deep/b.py", "b.py", true},
		{"**/*.go", "pkg/x.rs", "x.rs", false},
		{"*.go", "a.go", "a.go", true},          // bare pattern → basename
		{"*.go", "pkg/deep/c.go", "c.go", true}, // basename matches at any depth
		{"src/*.go", "src/a.go", "a.go", true},
		{"src/*.go", "other/a.go", "a.go", false},
	}
	for _, c := range cases {
		if got := grepGlobMatch(c.globs, "/wd", filepath.Join("/wd", c.rel), c.base); got != c.want {
			t.Errorf("grepGlobMatch(%q, %q) = %v, want %v", c.globs, c.rel, got, c.want)
		}
	}
}
