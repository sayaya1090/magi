// Package shortdir makes a temporary directory that a unix socket can actually live in.
//
// A unix address holds about 100 bytes — the same limit `daemon.TooLong` reports and the reason
// `MAGI_SOCKET_DIR` exists at all. `t.TempDir()` is not always inside it: on macOS it starts with
// `/var/folders/…/T/` and then appends the test's own name, and a name like
// `TestTargetAcceptsAnyListedDaemonAndNothingElse` is 45 bytes of that budget by itself. Binding
// there fails with "invalid argument", which says nothing whatsoever about length — observed
// exactly that way.
//
// So tests that bind a socket need a SHORT base, and three packages each grew their own
// `shortTempDir` with `/tmp` written into it. That worked on Linux and macOS and made the whole Go
// suite unrunnable on Windows, where `/tmp` does not exist: 24 tests in `clients/web/server` alone
// failed with `GetFileAttributesEx /tmp: The system cannot find the file specified`, none of them
// about anything the person had changed. CI is ubuntu, so nothing ever said so — measured
// 2026-09-10 on Windows 11.
//
// One helper, because the fix is one rule and this tree has already paid for the same fact living
// in three files.
package shortdir

import (
	"os"
	"path/filepath"
	"runtime"
)

// Make creates a temporary directory under a base short enough for a socket address.
//
// A drop-in for `os.MkdirTemp("/tmp", prefix)`: same signature, same contract, and the caller still
// owns removing it.
func Make(prefix string) (string, error) {
	return os.MkdirTemp(Base(), prefix)
}

// Base is the shortest directory this platform has that a socket can be bound in.
//
// `/tmp` where it exists — that is the whole point, and on Linux and macOS it is both short and
// always there. Windows has no `/tmp`, and `os.TempDir()` there is `%LocalAppData%\Temp`, which is
// short enough and is the ONE place under `AppData` where an AF_UNIX connect actually succeeds:
// everywhere else in that tree binds fine and then refuses every connection with WSAEINVAL, with
// the socket file left behind undeletable (measured 2026-09-09, `clients/visualstudio/docs/DESIGN`
// §5). So the platform's own temp is not a fallback here, it is the right answer.
func Base() string {
	if runtime.GOOS != "windows" {
		if fi, err := os.Stat("/tmp"); err == nil && fi.IsDir() {
			return "/tmp"
		}
	}
	return filepath.Clean(os.TempDir())
}
