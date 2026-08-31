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
