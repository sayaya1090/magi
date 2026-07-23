package main

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"
)

// materializeEmbedded must not rewrite a file whose on-disk copy already matches
// the embedded bytes: an identical rewrite still emits an fsnotify event, and a
// second magi instance sharing this config dir watches these paths — a spurious
// rewrite hot-reloads the plugin in the OTHER instance and drops its base-URL
// redirect. A changed file MUST still be written. mtime is the observable proxy.
func TestMaterializeEmbeddedSkipsIdenticalFiles(t *testing.T) {
	src := fstest.MapFS{
		"p/plugin.toml": {Data: []byte("name = \"p\"\n")},
		"p/init.lua":    {Data: []byte("-- v1\n")},
		"p/lib/x.lua":   {Data: []byte("return 1\n")},
	}
	dir := t.TempDir()

	if err := materializeEmbedded(src, "p", dir); err != nil {
		t.Fatalf("first materialize: %v", err)
	}
	// Backdate every materialized file so a rewrite is detectable by a newer mtime.
	old := time.Now().Add(-time.Hour)
	var paths []string
	for _, rel := range []string{"plugin.toml", "init.lua", "lib/x.lua"} {
		p := filepath.Join(dir, rel)
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatalf("chtimes %s: %v", rel, err)
		}
		paths = append(paths, p)
	}

	// A materialize from IDENTICAL bytes must touch nothing.
	if err := materializeEmbedded(src, "p", dir); err != nil {
		t.Fatalf("second materialize: %v", err)
	}
	for _, p := range paths {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatalf("stat %s: %v", p, err)
		}
		if fi.ModTime().After(old) {
			t.Errorf("identical file rewritten (mtime advanced): %s", p)
		}
	}

	// A CHANGED embedded file must be written (mtime advances); untouched files stay.
	changed := fstest.MapFS{
		"p/plugin.toml": {Data: []byte("name = \"p\"\n")},
		"p/init.lua":    {Data: []byte("-- v2 changed\n")},
		"p/lib/x.lua":   {Data: []byte("return 1\n")},
	}
	if err := materializeEmbedded(changed, "p", dir); err != nil {
		t.Fatalf("third materialize: %v", err)
	}
	if fi, _ := os.Stat(filepath.Join(dir, "init.lua")); !fi.ModTime().After(old) {
		t.Errorf("changed file was not rewritten")
	}
	if fi, _ := os.Stat(filepath.Join(dir, "plugin.toml")); fi.ModTime().After(old) {
		t.Errorf("unchanged sibling rewritten alongside a changed file")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "init.lua")); string(b) != "-- v2 changed\n" {
		t.Errorf("changed content not persisted, got %q", b)
	}
}
