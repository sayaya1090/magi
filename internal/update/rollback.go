package update

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	if err := os.WriteFile(prev, cur, 0o755); err != nil {
		return nil, nil, fmt.Errorf("save previous binary: %w", err)
	}
	restore = func() error {
		b, rerr := os.ReadFile(prev)
		if rerr != nil {
			return rerr
		}
		return Apply(b, abs) // atomic swap back, the same way the new one went in
	}
	discard = func() { _ = os.Remove(prev) }
	return restore, discard, nil
}

// Commit applies newBin over target, then verifies the result actually runs; if it does not, it
// restores the binary that was there before and returns the error. The on-disk binary is therefore
// only ever left as one that has PASSED the pre-flight — a bad build never becomes the one the daemon
// would restart into. On success the previous copy is discarded and the caller may restart (see
// internal/graceful). This is the rollback the self-update relies on.
func Commit(newBin []byte, target string) error {
	restore, discard, err := KeepPrevious(target)
	if err != nil {
		return err
	}
	if err := Apply(newBin, target); err != nil {
		discard() // Apply is atomic; nothing took, so just drop the saved copy
		return err
	}
	if err := Verify(target); err != nil {
		if rerr := restore(); rerr != nil {
			return fmt.Errorf("the new binary failed pre-flight (%v) and could not be rolled back (%v) — "+
				"the binary on disk may be broken; restore it by hand", err, rerr)
		}
		discard()
		return fmt.Errorf("update rolled back, the previous build is restored: %w", err)
	}
	discard()
	return nil
}
