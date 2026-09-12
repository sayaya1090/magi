package main

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// buildMagi builds this command into dir and returns a path that will actually run.
//
// ⚠ **`filepath.Join(dir, "magi")` is not a path Go will execute on Windows.** `go build -o` writes
// the file under exactly the name it is given, and a shell runs it happily — but `exec.Command`
// does not: on Windows it resolves the name through the extension list (.exe, .com, .bat, .cmd),
// finds nothing beside a file called `magi`, and fails before starting anything. Measured
// 2026-09-11 in one run:
//
//	magi      → exec: "…\magi": executable file not found in %PATH%   exit -1
//	magi.exe  → exit 0
//
// What that cost was worse than a red test. `ProcessState.ExitCode()` is -1 for a process that
// never started, so TestTheOwnedFlagRefusesWhatWouldMakeItALie reported `exit -1, want 2` and then
// `사유를 안 말한다` — the binary accused of not explaining its own refusal, by a test that never
// managed to ask it. The refusals were correct all along:
//
//	magi: --client-owned is a way to run a daemon — use it with --daemon
//	magi: --client-owned and --detach mean opposite things — …
//
// So the name is decided once, here, and the build is checked by RUNNING what it produced. A
// fixture that cannot produce a runnable binary says that about itself instead of letting the next
// assertion phrase it as a defect in the product.
// flags are extra `go build` flags, for a test that needs two builds it can tell apart — the only
// use so far is `-ldflags` naming a version, so a rollback can be shown to have ended on the OTHER
// build rather than merely on other bytes. They go through here rather than into a second `go build`
// beside the test, which is what TestOnlyBuildMagiBuildsMagi refuses and rightly: the executable
// suffix and the does-it-actually-start check live here.
func buildMagi(t *testing.T, dir string, flags ...string) string {
	t.Helper()
	exe := filepath.Join(dir, "magi")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	args := append([]string{"build", "-o", exe}, flags...)
	if out, err := exec.Command("go", append(args, "github.com/sayaya1090/magi/cmd/magi")...).CombinedOutput(); err != nil {
		t.Fatalf("could not build magi: %v\n%s", err, out)
	}
	// Asked of the path the tests will use, not of the file on disk: what matters is that
	// exec.Command can start it, and that is the question that was answering no.
	if out, err := exec.Command(exe, "--version").CombinedOutput(); err != nil {
		t.Fatalf("built magi at %s but could not run it: %v\n%s", exe, err, out)
	}
	return exe
}
