package update

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// verifyTimeout bounds the pre-flight of a freshly-written binary. `--version` prints one line and
// exits, so a binary that has not answered in this long is not slow — it is hung on startup, which is
// exactly the failure the pre-flight exists to catch before a restart commits to it.
var verifyTimeout = 15 * time.Second

// verifyWaitDelay bounds how long CombinedOutput may block AFTER the timeout has killed the process —
// a hung binary that spawned a child (sh running a sleep, say) leaves the child holding the output
// pipe open, so Wait would otherwise block on the pipe until the child exits, not the parent. WaitDelay
// makes Wait return once the delay passes. Only ever hit on the pathological hang; a real --version
// exits long before the timeout, let alone this.
var verifyWaitDelay = 3 * time.Second

// Verify runs the binary at path as a subprocess and checks it starts and reports a version — a cheap
// pre-flight before a self-update commits to relaunching onto it. A binary that cannot exec (a corrupt
// download, the wrong architecture) or that crashes or hangs on startup fails here, so the update can
// roll back to the known-good build instead of restarting into one that will not come up.
//
// It proves the file is a RUNNABLE magi, not that it serves correctly as a daemon — the check is
// `--version`, which does no I/O beyond stdout, touches no config, and exits fast. Catching "starts
// but does not serve" would need a watchdog that outlives the restart; this catches the common,
// cheap-to-check failures (the download is broken, the build is for another platform) that a SHA256
// match does not.
func Verify(path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), verifyTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version")
	cmd.WaitDelay = verifyWaitDelay
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("the new binary did not answer --version within %s — it may hang on start", verifyTimeout)
	}
	if err != nil {
		return fmt.Errorf("the new binary failed --version: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	if strings.TrimSpace(string(out)) == "" {
		return fmt.Errorf("the new binary printed nothing for --version — it is not a runnable magi")
	}
	return nil
}

// KeepPrevious copies the binary now at path aside, before an update overwrites it, so the update can
// put it back if the new one fails to come up. It returns restore — swap the saved copy back over
// path, the rollback — and discard — delete the saved copy once the new binary has proven good.
//
// The saved copy is at path+".prev", owned by this caller. It is separate from the ".old" Apply keeps
// on Windows so the two update paths never fight over one file: Apply's is its own atomic-rename
// scratch, this one is the rollback source the caller decides the fate of.
func KeepPrevious(path string) (restore func() error, discard func(), err error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, nil, err
	}
	prev := abs + ".prev"
	cur, err := os.ReadFile(abs)
	if err != nil {
		return nil, nil, fmt.Errorf("read current binary: %w", err)
	}
	// Written atomically (temp + fsync + rename), for the same reason Apply installs that way: this
	// file is the ROLLBACK SOURCE, and a torn or unsynced .prev restored over the target would be a
	// broken binary delivered by the path whose whole job is recovering from one.
	if err := writeBinary(prev, cur); err != nil {
		return nil, nil, fmt.Errorf("save previous binary: %w", err)
	}
	restore = func() error {
		b, rerr := os.ReadFile(prev)
		if rerr != nil {
			return rerr
		}
		// Written directly (temp + rename over the target), NOT through Apply. Apply's Windows
		// branch renames the target aside to ".old" first — but during a self-update ".old" is the
		// file backing the RUNNING image (Apply put it there on the way in), which Windows will not
		// let a rename replace, so a rollback through Apply always failed there. The target at this
		// point is the just-rejected new binary — an ordinary, unlocked file on every platform — and
		// a plain rename over it works everywhere.
		return writeBinary(abs, b)
	}
	discard = func() { _ = os.Remove(prev) }
	return restore, discard, nil
}

// writeBinary writes an executable atomically: temp file in the same directory, fsync, chmod, rename
// over dest. The fsync matters — rename is metadata-durable before the data is on some filesystems,
// and a power cut after an unsynced "successful" install would leave a truncated binary at dest.
func writeBinary(dest string, b []byte) error {
	dir := filepath.Dir(dest)
	tmp, err := os.CreateTemp(dir, ".magi-bin-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, 0o755); err != nil {
		return err
	}
	return os.Rename(name, dest)
}

// Commit applies newBin over target, then verifies the result actually runs; if it does not, it
// restores the binary that was there before and returns the error. The on-disk binary is therefore
// only ever left as one that has PASSED the pre-flight — a bad build never becomes the one the daemon
// would restart into. On success the previous copy is discarded and the caller may restart (see
// internal/graceful). This is the rollback the self-update relies on.
// commitMu serializes Commit. Two updates can genuinely race in one daemon — the auto loop and a
// console button press, or two console tabs — and unserialized they fight over the one .prev file:
// one's discard deletes the other's rollback source mid-rollback, and one's KeepPrevious can save the
// just-installed NEW build as "previous". Package-level rather than per-target because a process only
// ever self-updates one binary; the second caller waits the seconds the first takes.
var commitMu sync.Mutex

func Commit(newBin []byte, target string) error {
	commitMu.Lock()
	defer commitMu.Unlock()
	// One absolute path for every step. Verify was handed the caller's raw string, and a bare name
	// ("magi") would have made exec do a $PATH lookup — pre-flighting whatever binary is on PATH
	// instead of the one just written. Symlinks resolved too: an install like bin/magi → somewhere
	// would otherwise have the LINK replaced by a regular file instead of the binary behind it.
	abs, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	if resolved, rerr := filepath.EvalSymlinks(abs); rerr == nil {
		abs = resolved
	}
	// Idempotent: a second updater queued on the mutex (the auto loop and a console press racing)
	// arrives after the first already installed these exact bytes. Without this it would save the
	// NEW build as ".prev" and re-verify — and a transiently failing second Verify then "rolled
	// back" to the build that had just failed, reporting it as a restored previous.
	if cur, rerr := os.ReadFile(abs); rerr == nil && bytes.Equal(cur, newBin) {
		return nil
	}
	restore, discard, err := KeepPrevious(abs)
	if err != nil {
		return err
	}
	if err := Apply(newBin, abs); err != nil {
		discard() // Apply is atomic; nothing took, so just drop the saved copy
		return err
	}
	if err := Verify(abs); err != nil {
		if rerr := restore(); rerr != nil {
			// Only claim the .prev copy is there if it actually is: the likeliest restore failure IS
			// that file being unreadable, and pointing the operator at a file that is not there
			// makes a bad moment worse.
			where := "no saved copy survived — reinstall by hand"
			if _, serr := os.Stat(abs + ".prev"); serr == nil {
				where = "the previous build is beside it at " + abs + ".prev"
			}
			return fmt.Errorf("the new binary failed pre-flight (%v) and could not be rolled back (%v) — "+
				"the binary on disk may be broken; %s", err, rerr, where)
		}
		discard()
		return fmt.Errorf("update rolled back, the previous build is restored: %w", err)
	}
	discard()
	return nil
}
