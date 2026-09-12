package main

import (
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/testenv"
)

// diesAsEnv turns this test binary into a build that dies before it can serve: copied into an
// install path, it is the bad candidate a live test needs, and it exits with the code named here
// before touching anything. Checked before everything else in TestMain for that reason — a candidate
// that isolated a config tree first would not be dying "on its first line".
//
// A hook rather than a second program built beside the tests: building one means `go build` in a
// test file, which TestOnlyBuildMagiBuildsMagi rightly refuses, and this binary is already here.
const diesAsEnv = "MAGI_TEST_DIES_AS"

// ownsEnv turns this test binary into somebody's OWNER: it starts an owned daemon, keeps the write
// end of that daemon's stdin pipe, and then does nothing until it is killed.
//
// A test cannot be this owner itself, because the question is what happens when the owner is killed
// WITHOUT getting to close anything — an extension host being force-stopped, which is the case
// CLIENT_LIFECYCLE §4 names and nothing here measures. Killing the test process would end the test.
// So the owner is a process of its own, and the only thing that survives its death is what the
// operating system does with its handles.
const (
	ownsEnv    = "MAGI_TEST_OWNS"    // the magi binary to start
	ownsWSEnv  = "MAGI_TEST_OWNS_WS" // the workspace to start it in
	ownsCfgEnv = "MAGI_TEST_OWNS_CFG"
	ownsLogEnv = "MAGI_TEST_OWNS_LOG"
)

// 이 시험들은 제 설정 디렉토리를 짓는다. 주변 환경이 그것을 무르게 두면
// 사람의 진짜 magi 를 읽고 쓰게 된다 — 이유는 testenv 에 있다.
func TestMain(m *testing.M) {
	if code, err := strconv.Atoi(os.Getenv(diesAsEnv)); err == nil {
		os.Exit(code)
	}
	if exe := os.Getenv(ownsEnv); exe != "" {
		ownUntilKilled(exe)
	}
	testenv.Isolate()
	os.Exit(m.Run())
}

// ownUntilKilled starts an owned daemon and holds its pipe. It never returns: the test that started
// this process kills it, and the point is what that does to the daemon.
func ownUntilKilled(exe string) {
	r, w, err := os.Pipe()
	if err != nil {
		os.Exit(10)
	}
	log, err := os.Create(os.Getenv(ownsLogEnv))
	if err != nil {
		os.Exit(11)
	}
	cmd := exec.Command(exe, "--daemon", "--client-owned", "--no-update-check")
	cmd.Dir = os.Getenv(ownsWSEnv)
	cmd.Env = append(os.Environ(),
		ownsEnv+"=", // the daemon must not read this and start owning something itself
		"MAGI_CONFIG_DIR="+os.Getenv(ownsCfgEnv),
		"MAGI_SOCKET_DIR="+os.Getenv(ownsCfgEnv))
	// The child gets the READ end; this process keeps the write end and hands it to nobody. That is
	// the whole of the ownership — see the pipe note in cmd/magi's daemon loop.
	cmd.Stdin, cmd.Stdout, cmd.Stderr = r, log, log
	if err := cmd.Start(); err != nil {
		os.Exit(12)
	}
	r.Close() // the child has its own copy; EOF is about WRITE ends, and this process holds that
	_ = w     // never closed on purpose: only this process dying closes it, which is the point
	for {
		time.Sleep(time.Hour)
	}
}
