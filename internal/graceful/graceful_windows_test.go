//go:build windows

package graceful

import (
	"syscall"
	"testing"

	"golang.org/x/sys/windows"

	"github.com/sayaya1090/magi/internal/quietconsole"
)

func pretendConsole(t *testing.T, has bool) {
	t.Helper()
	was := quietconsole.HasConsole
	quietconsole.HasConsole = func() bool { return has }
	t.Cleanup(func() { quietconsole.HasConsole = was })
}

// A daemon with no console must not hand its successor a visible one. The black "magi.exe" window that
// appeared on every self-restart (2026-09-06) was exactly this: a console child of a console-less parent.
func TestASuccessorOfAConsolelessDaemonStaysConsoleless(t *testing.T) {
	pretendConsole(t, false)
	attr := reexecAttr()
	if attr.CreationFlags&windows.DETACHED_PROCESS == 0 {
		t.Fatalf("콘솔 없는 부모의 후임이 DETACHED 가 아니다: %#x", attr.CreationFlags)
	}
	if attr.CreationFlags&syscall.CREATE_NEW_PROCESS_GROUP == 0 {
		t.Fatalf("Ctrl-C 를 피하는 프로세스 그룹 플래그가 사라졌다: %#x", attr.CreationFlags)
	}
}

// In a terminal nothing changes: the successor inherits the console so the person keeps watching it.
func TestASuccessorInATerminalKeepsTheConsole(t *testing.T) {
	pretendConsole(t, true)
	attr := reexecAttr()
	if attr.CreationFlags&windows.DETACHED_PROCESS != 0 {
		t.Fatalf("터미널의 후임을 콘솔에서 떼어 냈다: %#x", attr.CreationFlags)
	}
	if attr.CreationFlags != syscall.CREATE_NEW_PROCESS_GROUP {
		t.Fatalf("플래그가 앞 판본과 다르다: %#x", attr.CreationFlags)
	}
}
