package main

import (
	"os"
	"path/filepath"
	"strings"
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

// A file being replaced must never be READABLE as half of itself.
//
// Several magi instances share one config directory — that is the ordinary shape now that magi runs
// as a daemon — and each materializes the embedded plugins at startup. os.WriteFile truncates and
// then writes, so between those two calls the file exists and is empty. A second instance that
// loads the plugin in that window reads a manifest with no name in it and reports exactly that:
// "manifest missing name", which describes the bytes and not the cause. Seen starting three daemons
// at once into a fresh config directory.
//
// The check is a reader spinning on the file while writers replace it: every read must be either
// the old content or the new one. Fails on truncate-then-write, and does so within a few thousand
// reads on any machine.
func TestMaterializeEmbeddedIsNeverReadableHalfWritten(t *testing.T) {
	dir := t.TempDir()
	v1 := fstest.MapFS{"p/plugin.toml": {Data: []byte("name = \"p\"\nversion = \"1\"\n")}}
	v2 := fstest.MapFS{"p/plugin.toml": {Data: []byte("name = \"p\"\nversion = \"2\"\n")}}
	if err := materializeEmbedded(v1, "p", dir); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "plugin.toml")
	want := map[string]bool{
		string(v1["p/plugin.toml"].Data): true,
		string(v2["p/plugin.toml"].Data): true,
	}

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() { // the writers: two instances materializing alternating versions
		defer close(done)
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			src := v1
			if i%2 == 1 {
				src = v2
			}
			if err := materializeEmbedded(src, "p", dir); err != nil {
				t.Errorf("materialize: %v", err)
				return
			}
		}
	}()

	var bad string
	for i := 0; i < 20000 && bad == ""; i++ {
		b, err := os.ReadFile(target)
		if err != nil {
			bad = "the file disappeared mid-replace: " + err.Error()
			break
		}
		if !want[string(b)] {
			bad = "read a partial manifest: " + string(b)
		}
	}
	close(stop)
	<-done
	if bad != "" {
		t.Error(bad)
	}
	// And the survivor is still one of the two whole versions, not a leftover temp.
	b, err := os.ReadFile(target)
	if err != nil || !want[string(b)] {
		t.Errorf("after the race the file is %q (%v)", string(b), err)
	}
	// No temp files left behind: a config directory that fills with .plugin.toml.tmp* is its own
	// defect, and a plugin loader that walks the directory would try to read them.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") && strings.Contains(e.Name(), ".tmp") {
			t.Errorf("a temp file was left behind: %s", e.Name())
		}
	}
}
