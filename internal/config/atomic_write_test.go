package config

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/sayaya1090/magi/internal/atomicfile"
)

// A reader landing mid-edit sees the old config or the new one, never a blank: the write used to
// truncate-then-write, and a torn read decoded as an errorless zero Config — no model, no
// permission, empty allow — which is worse than an error (hunted with a 4×3000 probe: 324 blank
// reads before the fix).
func TestConcurrentEditNeverServesABlankConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("model = \"seed\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			if err := SetKey(path, "", "model", "m"+string(rune('a'+i%26))); err != nil {
				t.Errorf("set: %v", err)
				return
			}
		}
	}()
	blank := 0
	for i := 0; i < 3000; i++ {
		// Read through the door a load reads through, not around it. On Windows a read landing
		// mid-replacement sees neither version — ERROR_SHARING_VIOLATION — and atomicfile.ReadFile
		// is where that window is waited out. os.ReadFile here measured the platform instead of
		// the write, and did: this failed on Windows for a reason that had nothing to do with the
		// truncate-then-write it was written for.
		b, err := atomicfile.ReadFile(path)
		if err != nil || len(b) == 0 {
			blank++
		}
	}
	close(stop)
	wg.Wait()
	if blank > 0 {
		t.Fatalf("%d of 3000 reads landed on a blank or missing config", blank)
	}
}

// Loading a config never fails because somebody else is editing it.
//
// ⚠ **The writers take a lock and the readers deliberately do not.** withFileLock serializes edits
// across processes; a load must not queue behind one, because loading is what every companion does
// at startup and on every reload. That makes the reader the side that has to survive the
// replacement — and on Windows there is one to survive: config.toml is published by rename, and a
// read landing in that window sees neither version. It fails with ERROR_SHARING_VIOLATION, which
// is not os.IsNotExist, so LoadWithUnknown handed it to the caller as an error about a file that
// was whole the entire time.
//
// Measured 2026-09-11 before the fix: 94 of 3000 loads, 3.1%, while a sibling ran SetKey in a
// loop. After: 0.
//
// Asked through Load rather than through the file, because the file is not what was broken. The
// test above this one reads the bytes and proves the write is whole; this asks the question every
// startup asks, and that is the one that was answering wrong.
func TestLoadingNeverFailsBecauseSomebodyElseIsEditing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("model = \"seed\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			if err := SetKey(path, "", "model", "m"+string(rune('a'+i%26))); err != nil {
				t.Errorf("set: %v", err)
				return
			}
		}
	}()
	var failed, blank int
	const rounds = 3000
	for i := 0; i < rounds; i++ {
		c, err := Load(dir)
		switch {
		case err != nil:
			failed++
		case c.Model == "":
			blank++
		}
	}
	close(stop)
	wg.Wait()
	if failed > 0 {
		t.Errorf("%d of %d loads failed while a sibling edited the file — the config was whole "+
			"throughout; the read is not going through atomicfile.ReadFile", failed, rounds)
	}
	if blank > 0 {
		t.Errorf("%d of %d loads decoded to a config with no model in it", blank, rounds)
	}
}
