//go:build !windows

// 이 시험 하나만 유닉스의 것이다 — 아이가 아직 사는지 `syscall.Kill(pid, 0)` 으로
// 묻는데, 그 함수는 윈도우에 없다. 태그가 없어서 패키지 전체가 윈도우에서
// 컴파일조차 안 됐고, 그래서 같은 파일의 나머지 일곱도 함께 안 돌았다.
//
// 파일 전체에 태그를 다는 대신 이 하나만 뗀 이유가 그것이다: 안 도는 이유가
// 「이 시험이 유닉스의 것」이지 「이 파일이 유닉스의 것」이 아니었다.

package lua

import (
	"strings"
	"syscall"
	"testing"
	"time"
)

// Unloading the plugin kills what it left running. A child that outlives its plugin is a process
// nobody owns and nobody will ever close.
func TestPipeChildDiesWithThePlugin(t *testing.T) {
	h, out, err := loadPiped(t,
		`name="pipey"`+"\n"+`permissions=["exec:cat"]`,
		`local ch = magi.pipe("cat")
magi.log("pid=" .. tostring(ch.pid))`,
	)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	pid := 0
	if i := strings.Index(out, "pid="); i >= 0 {
		for _, c := range out[i+4:] {
			if c < '0' || c > '9' {
				break
			}
			pid = pid*10 + int(c-'0')
		}
	}
	if pid == 0 {
		t.Fatalf("no pid in log: %q", out)
	}
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("child should be running before unload: %v", err)
	}
	if err := h.Unload("pipey"); err != nil {
		t.Fatalf("Unload: %v", err)
	}
	// The kill is delivered synchronously; the reap is not, so give the OS a moment to make the
	// pid unreachable rather than asserting on the instant.
	gone := false
	for i := 0; i < 50; i++ {
		if err := syscall.Kill(pid, 0); err != nil {
			gone = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !gone {
		_ = syscall.Kill(pid, syscall.SIGKILL) // do not leave it behind either way
		t.Error("the child should not outlive its plugin")
	}
}
