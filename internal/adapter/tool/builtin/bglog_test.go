package builtin

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A child must be able to WRITE to the handle its output goes to.
//
// That reads like nothing to check. It was not: on Windows `O_APPEND` produces a handle opened with
// `FILE_APPEND_DATA` and without `FILE_WRITE_DATA`, and the MSYS/Cygwin runtime that Git for
// Windows links its coreutils against cannot write to one. `printf`, `sed`, `awk`, `head` — every
// one of them exited 1 having produced nothing, and the tool reported `[bg_1 exited 1]` with an
// empty log, which reads exactly like a command that genuinely failed. Measured 2026-09-10.
//
// The foreground path writes to a pipe and was fine, so one command succeeded or vanished depending
// on `background` — a flag the caller does not think is about this.
//
// **The handle is the only variable.** The same command is run twice: once to a pipe, once to the
// log handle. The pipe run says whether this machine can run the command at all — if it cannot,
// there is nothing here to measure and the test says so instead of blaming the handle. Only when
// the pipe run works does the log run have to work too.
//
// Written through the seam rather than against the flags it passes. Flags are how it is done today;
// that a child can write is what has to stay true.
func TestABackgroundLogTakesWhatAChildWrites(t *testing.T) {
	// `printf` on purpose: on Windows it comes from the MSYS install, which is the runtime the
	// handle broke. A shell builtin would have passed either way and measured nothing.
	name, args := shell(`printf 'magi-bg-probe\n'`)

	through, perr := exec.Command(name, args...).CombinedOutput()
	if perr != nil || !strings.Contains(string(through), "magi-bg-probe") {
		t.Skipf("이 기계에서는 이 명령 자체가 안 돈다 (%v, %q) — 핸들에 대해 잴 것이 없다", perr, string(through))
	}

	log := filepath.Join(t.TempDir(), "bg.log")
	if err := os.WriteFile(log, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := openBackgroundLog(log)
	if err != nil {
		t.Fatalf("openBackgroundLog: %v", err)
	}
	cmd := exec.Command(name, args...)
	cmd.Stdout, cmd.Stderr = f, f
	runErr := cmd.Run()
	_ = f.Close()

	body, rerr := os.ReadFile(log)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if runErr != nil {
		t.Fatalf("파이프로는 도는 명령이 로그 핸들에 대고는 졌다: %v\n"+
			"배경 명령이 통째로 못 돈다는 뜻이다. 로그: %q", runErr, string(body))
	}
	if !strings.Contains(string(body), "magi-bg-probe") {
		t.Errorf("파이프로는 나오는 출력이 로그에는 없다: %q", string(body))
	}
}
