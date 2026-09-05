//go:build windows

package quietconsole

import (
	"os/exec"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
)

func pretendConsole(t *testing.T, has bool) {
	t.Helper()
	was := HasConsole
	HasConsole = func() bool { return has }
	t.Cleanup(func() { HasConsole = was })
}

// The daemon case: no console of our own → the child gets an invisible one. Whatever the caller
// already put in the attributes (a sandbox token) survives.
func TestWithoutAConsoleTheChildGetsNoWindow(t *testing.T) {
	pretendConsole(t, false)
	attr := Attr(&syscall.SysProcAttr{Token: syscall.Token(7)})
	if !attr.HideWindow || attr.CreationFlags&windows.CREATE_NO_WINDOW == 0 {
		t.Fatalf("창을 안 숨겼다: %+v", attr)
	}
	if attr.Token != syscall.Token(7) {
		t.Fatalf("샌드박스 토큰이 사라졌다: %+v", attr)
	}
	cmd := exec.Command("cmd", "/c", "echo")
	Apply(cmd)
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.HideWindow {
		t.Fatalf("Apply 가 nil 속성에서 안 만들었다: %+v", cmd.SysProcAttr)
	}
}

// The TUI case: we sit in a terminal → nothing changes, not even a nil → non-nil promotion, so the
// child inherits the console exactly as before (interactive programs that open CONIN$ rely on it).
func TestWithAConsoleNothingChanges(t *testing.T) {
	pretendConsole(t, true)
	if got := Attr(nil); got != nil {
		t.Fatalf("콘솔이 있는데 속성을 만들었다: %+v", got)
	}
	cmd := exec.Command("cmd", "/c", "echo")
	Apply(cmd)
	if cmd.SysProcAttr != nil {
		t.Fatalf("콘솔이 있는데 속성을 만들었다: %+v", cmd.SysProcAttr)
	}
}

// The whole point of choosing CREATE_NO_WINDOW over DETACHED_PROCESS: output still comes back
// through the pipes. Runs a real console child the way the bash tool does.
func TestOutputStillFlowsThroughThePipes(t *testing.T) {
	pretendConsole(t, false)
	cmd := exec.Command("powershell", "-NoProfile", "-Command", "echo hello")
	Apply(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("숨긴 콘솔로 띄운 자식이 실패했다: %v — %s", err, out)
	}
	if strings.TrimSpace(string(out)) != "hello" {
		t.Fatalf("출력이 파이프로 안 왔다: %q", out)
	}
}
