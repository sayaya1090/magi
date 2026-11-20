//go:build !windows

package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/port"
)

// A person's message while wait_for waits ends the wait at once (2026-11-17: "when wait_for runs you
// should be back where you can take my message"). The wait says so; the job is not touched.
func TestWaitForReturnsWhenAMessageArrives(t *testing.T) {
	wake := make(chan struct{})
	env := port.ToolEnv{Workdir: t.TempDir(), PromptArrived: func() <-chan struct{} { return wake }}
	go func() { time.Sleep(200 * time.Millisecond); close(wake) }()
	start := time.Now()
	res, err := WaitFor{}.Execute(context.Background(), json.RawMessage(`{"condition":"false","timeout":30,"interval":5}`), env)
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("the wait did not end on the message: %s", d)
	}
	var s string
	_ = json.Unmarshal(res.Content, &s)
	if !strings.Contains(s, "message") {
		t.Errorf("the result must say a message arrived: %q", s)
	}
}

// A message that lands while a check is running ends the wait then, not when the check finishes: a
// check may run up to a minute. The check is stopped (its whole process group); the wait says so.
func TestWaitForReturnsWhenAMessageArrivesDuringACheck(t *testing.T) {
	wake := make(chan struct{})
	dir := t.TempDir()
	env := port.ToolEnv{Workdir: dir, PromptArrived: func() <-chan struct{} { return wake }}
	go func() { time.Sleep(300 * time.Millisecond); close(wake) }()
	start := time.Now()
	res, err := WaitFor{}.Execute(context.Background(), json.RawMessage(`{"condition":"sleep 3 & echo $! > child; wait $!; false","timeout":30,"interval":1}`), env)
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 1500*time.Millisecond {
		t.Fatalf("the wait waited for the running check to finish: %s", d)
	}
	var s string
	_ = json.Unmarshal(res.Content, &s)
	if !strings.Contains(s, "message") || !strings.Contains(s, "the check that was running was stopped") {
		t.Errorf("the result must say a message arrived and the check was stopped: %q", s)
	}
	// The check's child goes with it, not only its shell.
	raw, err := os.ReadFile(filepath.Join(dir, "child"))
	if err != nil {
		t.Fatalf("the check never started its child: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	gone := false
	for i := 0; i < 20 && !gone; i++ {
		gone = syscall.Kill(pid, 0) == syscall.ESRCH
		if !gone {
			time.Sleep(50 * time.Millisecond)
		}
	}
	if !gone {
		syscall.Kill(pid, syscall.SIGKILL)
		t.Fatalf("the stopped check left its child %d running", pid)
	}
}
