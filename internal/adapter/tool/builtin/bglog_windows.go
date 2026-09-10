package builtin

import "os"

// openBackgroundLog opens the file a background command's output is written to.
//
// ⚠ **No O_APPEND here, and the reason is that it breaks whole programs.**
//
// Windows has no append MODE — it has an append RIGHT. Asking for O_APPEND gets a handle opened
// with `FILE_APPEND_DATA` and *without* `FILE_WRITE_DATA`, and the MSYS/Cygwin runtime that Git for
// Windows links its coreutils against cannot write to such a handle: the program exits 1 having
// produced nothing at all.
//
// Measured 2026-09-10, the same command with only the log handle changed:
//
//	O_WRONLY|O_APPEND   printf 'a\nb\n'   → exit 1, log empty
//	O_WRONLY            printf 'a\nb\n'   → exit 0, log "a\nb\n"
//
// `printf` is not an unusual thing to reach for, and it is not alone: `sed`, `awk`, `head`, `sort`
// and the rest of that install are the same binaries. And the failure is split by a flag the caller
// did not think was about this — the FOREGROUND path writes to a pipe and works, so one command
// succeeds or vanishes depending on `background`. Nothing says why; the model reads `exited 1` with
// no output and has no way to tell that from a command that genuinely failed.
//
// **What is given up.** rotateIfHuge truncates this file when it passes the disk cap, and without
// append the child's handle keeps its old offset — so the next write lands past a new EOF and
// Windows fills the gap with NUL bytes. That costs a run that both exceeds the cap AND is still
// writing: one padded gap in a log that had just been truncated anyway. The other way costs every
// MSYS program, immediately, on the first call. Both halves are written down because the trade is
// real; it is not that O_APPEND was pointless here.
//
// The two rights cannot be combined: a handle holding FILE_WRITE_DATA as well is an ordinary write
// handle and no longer appends, so there is no spelling that keeps both.
func openBackgroundLog(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY, 0o600)
}
