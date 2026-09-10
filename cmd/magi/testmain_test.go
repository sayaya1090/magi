package main

import (
	"os"
	"testing"

	"github.com/sayaya1090/magi/internal/testenv"
)

// 이 시험들은 제 설정 디렉토리를 짓는다. 주변 환경이 그것을 무르게 두면
// 사람의 진짜 magi 를 읽고 쓰게 된다 — 이유는 testenv 에 있다.
func TestMain(m *testing.M) {
	testenv.Isolate()
	os.Exit(m.Run())
}
