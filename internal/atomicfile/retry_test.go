package atomicfile

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A file that is not there is an ANSWER, and it comes back at once.
//
// ⚠ **The retry is for a replacement in flight, and "not there" is not one.** On Windows ReadFile
// waits out the window where a destination cannot be opened, and the list of errors it waits on
// once included fs.ErrNotExist — defensively, on the reasoning that a replacement surely has a
// moment where the file is gone. It does not: os.Rename there is MoveFileEx with REPLACE_EXISTING,
// which swaps the name rather than unlinking it first. Measured 2026-09-11, a reader spinning
// through three seconds of continuous replacement: 16,498 whole reads, 613 sharing violations,
// ENOENT zero times.
//
// The cost was on the other side, and it was not hypothetical. A record that does not exist is an
// ORDINARY state here — daemon.List draws a socket with no record as "(unknown — no record)",
// because something is listening there either way — and each one burned the whole 200ms budget
// waiting for a file nobody was writing, on a list the TUI refreshes every two seconds. 204ms
// against 606µs, per absent record, per refresh.
//
// The bound is deliberately far below that budget and far above any real open: this asks whether
// the retry loop is entered at all, not how fast the disk is.
func TestAMissingFileComesBackAtOnce(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such.session")
	start := time.Now()
	if _, err := ReadFile(missing); err == nil {
		t.Fatal("reading a file that is not there must fail")
	} else if !os.IsNotExist(err) {
		t.Fatalf("it must fail AS not-exist, so callers can tell it apart: %v", err)
	}
	if d := time.Since(start); d > 50*time.Millisecond {
		t.Errorf("a missing file took %v — the retry budget is being spent on a file nobody is "+
			"writing; fs.ErrNotExist is back in transient()", d.Round(time.Millisecond))
	}
}

// And the wait it DOES exist for still happens: a reader never sees half a file.
//
// Held next to the test above because the two are one decision. Narrowing what counts as transient
// is only correct while the replacement window is still covered, and the way that stops being true
// is somebody narrowing it one error further.
func TestAReaderStillNeverSeesHalfAFileWhileItIsReplaced(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "rec.json")
	v1, v2 := []byte(`{"v":1}`), []byte(`{"v":2222222222}`)
	if err := Write(target, v1, 0o600); err != nil {
		t.Fatal(err)
	}
	whole := map[string]bool{string(v1): true, string(v2): true}

	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			b := v1
			if i%2 == 1 {
				b = v2
			}
			if err := Write(target, b, 0o600); err != nil {
				t.Errorf("write: %v", err)
				return
			}
		}
	}()
	var bad string
	for i := 0; i < 5000 && bad == ""; i++ {
		b, err := ReadFile(target)
		if err != nil {
			bad = "the file could not be read while it was being replaced: " + err.Error()
		} else if !whole[string(b)] {
			bad = "a partial file was read: " + string(b)
		}
	}
	close(stop)
	<-done
	if bad != "" {
		t.Error(bad)
	}
}
