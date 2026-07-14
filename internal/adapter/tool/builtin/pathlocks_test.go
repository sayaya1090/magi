package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/port"
)

// The per-path lock provides mutual exclusion for one key: many goroutines doing a
// read-increment-write of a shared counter under the lock must not lose an update,
// and the set must reclaim the entry once every user has released it.
func TestPathLockSerializesSameKey(t *testing.T) {
	s := &pathLockSet{m: map[string]*refMutex{}}
	const g = 50
	counter := 0
	var wg sync.WaitGroup
	for i := 0; i < g; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			unlock := s.lock("k")
			defer unlock()
			v := counter // read-modify-write under the lock
			counter = v + 1
		}()
	}
	wg.Wait()
	if counter != g {
		t.Fatalf("counter = %d, want %d (lost updates → lock not exclusive)", counter, g)
	}
	if len(s.m) != 0 {
		t.Fatalf("lock set retained %d entries, want 0 (refcount reclaim failed)", len(s.m))
	}
}

// Different keys must not block each other: holding key "a" cannot delay acquiring
// key "b". Guards against a coarse global lock sneaking in and serializing unrelated
// files.
func TestPathLockDistinctKeysDontBlock(t *testing.T) {
	s := &pathLockSet{m: map[string]*refMutex{}}
	release := s.lock("a")
	defer release()

	done := make(chan struct{})
	go func() {
		unlock := s.lock("b") // must proceed even though "a" is held
		unlock()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("acquiring a different key blocked on an unrelated held key")
	}
}

// End-to-end: many goroutines each edit a DISTINCT unique token in one shared file.
// Every edit is a full read-modify-write, so without the per-path lock the writes
// interleave and clobber each other; with it, all edits land. Run under -race.
func TestEditConcurrentNoLostUpdate(t *testing.T) {
	dir := t.TempDir()
	const n = 40
	var seed strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&seed, "L%02d\n", i) // zero-padded so no token is a substring of another
	}
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte(seed.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			raw, _ := json.Marshal(map[string]any{
				"path": "f.txt",
				"old":  fmt.Sprintf("L%02d", i),
				"new":  fmt.Sprintf("X%02d", i),
			})
			res, _ := Edit{}.Execute(context.Background(), raw, port.ToolEnv{Workdir: dir})
			if res.IsError {
				t.Errorf("edit %d failed: %s", i, res.Content)
			}
		}(i)
	}
	wg.Wait()

	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	for i := 0; i < n; i++ {
		if !strings.Contains(got, fmt.Sprintf("X%02d\n", i)) {
			t.Errorf("edit %d was lost — %q missing from final file", i, fmt.Sprintf("X%02d", i))
		}
	}
}
