//go:build !windows

package daemon

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// The socket is owner-only from the first instant it exists, not from a moment later.
//
// Chmod after Listen leaves a window, and it is not theoretical: a unix socket is created with the
// process umask — rwxr-xr-x under the usual 022 — and it accepts from the moment it exists, because
// the kernel queues connections before anyone calls Accept. Anything that connects in that window
// holds a channel whose purpose is to make this engine run commands as this user.
//
// So the check is on the listener BEFORE any chmod: whatever opens the socket must get the mode
// right itself.
func TestTheSocketIsOwnerOnlyBeforeAnyChmod(t *testing.T) {
	path := filepath.Join(shortDir(t), "perm.sock")
	ln, err := listenOwnerOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := fi.Mode().Perm(); mode&0o077 != 0 {
		t.Errorf("the socket is %v the moment it exists — group and other can reach a control "+
			"channel that runs commands", mode)
	}
	// And the umask is put back: leaving it flipped would silently narrow every file this process
	// writes afterwards, which is a change nobody asked for and nobody would look for.
	probe := filepath.Join(filepath.Dir(path), "after.txt")
	if err := os.WriteFile(probe, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	pfi, err := os.Stat(probe)
	if err != nil {
		t.Fatal(err)
	}
	if pfi.Mode().Perm() != fs.FileMode(0o644) {
		t.Errorf("a file written after the listen came out %v — the umask was not restored",
			pfi.Mode().Perm())
	}
}

// Two daemons starting at once must not restore each other's umask. The umask is per-process, so
// an unsynchronised flip leaves whichever finishes last holding a value it did not set — and every
// file the process writes after that comes out with the wrong mode.
func TestConcurrentListensLeaveTheUmaskAlone(t *testing.T) {
	dir := shortDir(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			p := filepath.Join(dir, "c"+string(rune('a'+i))+".sock")
			ln, err := listenOwnerOnly(p)
			if err != nil {
				t.Error(err)
				return
			}
			defer ln.Close()
			if fi, serr := os.Stat(p); serr == nil && fi.Mode().Perm()&0o077 != 0 {
				t.Errorf("%s came out %v", p, fi.Mode().Perm())
			}
		}(i)
	}
	wg.Wait()

	probe := filepath.Join(dir, "after.txt")
	if err := os.WriteFile(probe, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(probe)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != fs.FileMode(0o644) {
		t.Errorf("after eight concurrent listens a 0644 file came out %v", fi.Mode().Perm())
	}
}

// Whatever the path taken, a running daemon's socket and its record are owner-only. The record
// names the workspace and the session; the socket runs commands.
func TestAServingDaemonKeepsBothFilesPrivate(t *testing.T) {
	dir := shortDir(t)
	sock := filepath.Join(dir, "daemon-priv.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = Serve(ctx, &fakeEngine{}, sock) }()
	waitForSocket(t, sock)

	unpublish, err := Publish(sock, "/w", "s_1")
	if err != nil {
		t.Fatal(err)
	}
	defer unpublish()
	for _, p := range []string{sock, SessionFile(sock)} {
		fi, serr := os.Stat(p)
		if serr != nil {
			t.Fatal(serr)
		}
		if fi.Mode().Perm()&0o077 != 0 {
			t.Errorf("%s is %v — readable or writable by others", filepath.Base(p), fi.Mode().Perm())
		}
	}
}
