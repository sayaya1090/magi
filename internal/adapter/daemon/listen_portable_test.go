package daemon

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

// A daemon can bind its socket on THIS platform.
//
// Every other test in this package reaches for /tmp, which does not exist on Windows, so the whole
// suite skipped there — and under that skip `magi --daemon` could not start on Windows at all: the
// bind succeeded and the chmod that followed answered "The file cannot be accessed by the system",
// because an AF_UNIX socket is not a file Windows will set attributes on. The failure named a
// permission bit; the cause was the platform.
//
// So this test asks the smallest possible question, and asks it wherever it is run: can this
// platform stand a daemon up and take it down again. It uses the OS temp directory rather than a
// hard-coded /tmp for the same reason it exists.
func TestADaemonCanBindItsSocketOnThisPlatform(t *testing.T) {
	dir, err := os.MkdirTemp("", "mgl")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	path := filepath.Join(dir, "daemon-x.sock")
	d, err := Listen(path)
	if err != nil {
		t.Fatalf("이 플랫폼에서 데몬이 소켓을 못 잡았다: %v", err)
	}
	// 잡았으면 **다이얼이 닿아야** 한다 — 파일이 생긴 것과 리스너가 선 것은 다른 사실이다.
	c, derr := net.Dial("unix", path)
	if derr != nil {
		d.Close()
		t.Fatalf("소켓은 생겼는데 아무도 안 듣는다: %v", derr)
	}
	c.Close()
	if err := d.Close(); err != nil {
		t.Fatalf("닫는 데 실패했다: %v", err)
	}
}

// 닫은 데몬은 자기 소켓 파일을 **치우고 간다** — 그리고 그 자리를 다시 잡을 수 있어야 한다.
//
// 윈도우에서 이게 안 됐다. AF_UNIX 소켓이 **리파스 포인트**라 `os.Remove` 가
// ERROR_CANT_ACCESS_FILE 로 떨어지고, 남은 파일은 `del` 로도 `fsutil` 로도 안 지워졌다. 그러면
// 그 워크스페이스는 **영구히 못 쓴다**: 다음 기동의 복구 경로가 바로 그 remove 이고, 남은
// 파일로 가는 dial 은 「아무도 안 듣는다」가 아니라 「인자가 잘못됐다」로 답한다.
//
// 그래서 이 시험이 재는 것은 「지웠나」가 아니라 **「다시 설 수 있나」**다.
func TestAClosedDaemonLeavesTheWorkspaceReusable(t *testing.T) {
	dir, err := os.MkdirTemp("", "mgl")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "daemon-y.sock")

	first, err := Listen(path)
	if err != nil {
		t.Fatalf("첫 기동이 실패했다: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("닫는 데 실패했다: %v", err)
	}
	if _, serr := os.Lstat(path); !os.IsNotExist(serr) {
		t.Errorf("나가면서 소켓 파일을 안 치웠다: %v", serr)
	}
	second, err := Listen(path)
	if err != nil {
		t.Fatalf("같은 자리에 다시 못 섰다 — 이 워크스페이스는 이제 못 쓴다: %v", err)
	}
	second.Close()
}
