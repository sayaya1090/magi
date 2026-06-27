package platform

import (
	"context"
	"runtime"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

func TestExecCapturesOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX echo")
	}
	res, err := OS{}.Exec(context.Background(), port.Cmd{Path: "echo", Args: []string{"hello"}})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if got := strings.TrimSpace(string(res.Stdout)); got != "hello" {
		t.Errorf("stdout=%q want hello", got)
	}
	if res.ExitCode != 0 {
		t.Errorf("exit=%d want 0", res.ExitCode)
	}
}

func TestExecNonZeroExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX sh")
	}
	res, err := OS{}.Exec(context.Background(), port.Cmd{Path: "sh", Args: []string{"-c", "exit 3"}})
	if err != nil {
		t.Fatalf("Exec returned Go error for non-zero exit: %v", err)
	}
	if res.ExitCode != 3 {
		t.Errorf("exit=%d want 3", res.ExitCode)
	}
}

func TestDirs(t *testing.T) {
	if d := (OS{}).ConfigDir(); !strings.Contains(d, "magi") {
		t.Errorf("ConfigDir=%q should contain magi", d)
	}
	if d := (OS{}).DataDir(); !strings.Contains(d, "magi") {
		t.Errorf("DataDir=%q should contain magi", d)
	}
}

func TestTerminalCapsNoPanic(t *testing.T) {
	_ = OS{}.TerminalCaps() // detection must not panic regardless of env
}
