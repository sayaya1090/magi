package main

import (
	"os"
	"strconv"
	"testing"

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

// 이 시험들은 제 설정 디렉토리를 짓는다. 주변 환경이 그것을 무르게 두면
// 사람의 진짜 magi 를 읽고 쓰게 된다 — 이유는 testenv 에 있다.
func TestMain(m *testing.M) {
	if code, err := strconv.Atoi(os.Getenv(diesAsEnv)); err == nil {
		os.Exit(code)
	}
	testenv.Isolate()
	os.Exit(m.Run())
}
