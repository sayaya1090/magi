//go:build !windows

package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/shortdir"
)

// lockedBuf collects a process's output while the test reads it.
type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}
func (l *lockedBuf) String() string { l.mu.Lock(); defer l.mu.Unlock(); return l.b.String() }

// The terminal dies mid-task (2026-11-16: "SIGKILL, restart, and it starts a new conversation — a
// dead terminal loses the work"). The standalone TUI created a new session on every start and never
// ran startup recovery, so the cut conversation stayed unsettled and the screen opened empty. Now,
// when this TUI is the only one here and no daemon holds the workspace, the conversation that was
// cut is reopened. Real processes: a real SIGKILL mid-tool, a real TUI on a pseudo-terminal.
func TestATerminalKilledMidTaskReopensItsConversation(t *testing.T) {
	if !strings.Contains(terminalAfter(t, true), "RESUME_MARKER") {
		t.Fatal("the reopened terminal does not show the cut conversation; it opened another")
	}
}

// A conversation that ended normally is not reopened: a fresh start still opens a new one.
func TestATerminalStartedAfterAFinishedTaskOpensANewConversation(t *testing.T) {
	if strings.Contains(terminalAfter(t, false), "RESUME_MARKER") {
		t.Fatal("a finished conversation was reopened; only a cut one should be")
	}
}

// terminalAfter runs a task in a real magi — cut by a real SIGKILL while its tool runs, or left to
// finish — then starts a real TUI on a pseudo-terminal and returns what it drew.
func terminalAfter(t *testing.T, cut bool) string {
	t.Helper()
	if testing.Short() {
		t.Skip("starts real magi processes")
	}
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("needs script(1) for a pseudo-terminal")
	}
	if _, err := exec.LookPath("script"); err != nil {
		t.Skip("no script(1)")
	}
	exe := buildMagi(t, t.TempDir())
	root, err := shortdir.Make("mtr")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	cfg, ws, data := filepath.Join(root, "cfg"), filepath.Join(root, "ws"), filepath.Join(root, "data")
	for _, d := range []string{cfg, ws, data} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if ws, err = filepath.EvalSymlinks(ws); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			fmt.Fprint(w, `{"data":[{"id":"fixture","object":"model"}]}`)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if !bytes.Contains(body, []byte(`"messages"`)) {
			http.Error(w, "chat completions only", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if !cut {
			fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"done"},"finish_reason":"stop"}]}`+"\n\ndata: [DONE]\n\n")
			return
		}
		fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c_long","type":"function","function":{"name":"bash","arguments":"{\"command\":\"sleep 30\"}"}}]}}]}`+"\n\n")
		fmt.Fprint(w, `data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`+"\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)
	env := append(os.Environ(), "MAGI_CONFIG_DIR="+cfg, "MAGI_SOCKET_DIR="+cfg, "MAGI_DATA_DIR="+data,
		"MAGI_BASE_URL="+server.URL+"/v1", "MAGI_MODEL=fixture", "MAGI_API_KEY=x", "OPENAI_API_KEY=",
		"MAGI_PERMISSION=allow", "MAGI_WORKFLOW=false", "MAGI_EMBEDDED_PLUGINS=off", "TERM=xterm-256color")

	// 1. A task in flight, killed for real while its tool runs.
	run := exec.Command(exe, "--no-update-check", "-p", "RESUME_MARKER run the long build")
	run.Dir, run.Env = ws, env
	run.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := run.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { syscall.Kill(-run.Process.Pid, syscall.SIGKILL) })
	if !cut {
		if err := run.Wait(); err != nil {
			t.Fatalf("the finished task: %v", err)
		}
	} else {
		killMidTool(t, run, data)
	}
	return screenOfTUI(t, exe, ws, env, cut)
}

func killMidTool(t *testing.T, run *exec.Cmd, data string) {
	t.Helper()
	started := false
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline) && !started; time.Sleep(100 * time.Millisecond) {
		filepath.WalkDir(data, func(p string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() && strings.HasSuffix(p, ".jsonl") {
				if b, _ := os.ReadFile(p); bytes.Contains(b, []byte("tool.started")) {
					started = true
				}
			}
			return nil
		})
	}
	if !started {
		t.Fatal("the task never started its tool")
	}
	if err := run.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	run.Wait()
}

// screenOfTUI is what a real TUI drew in its first seconds. With want it returns as soon as the
// marker shows; without, it waits for the start screen to settle.
func screenOfTUI(t *testing.T, exe, ws string, env []string, want bool) string {
	t.Helper()
	inner := `stty cols 120 rows 40 2>/dev/null; exec "$0" --no-update-check`
	var tui *exec.Cmd
	if runtime.GOOS == "darwin" {
		tui = exec.Command("script", "-q", "/dev/null", "sh", "-c", inner, exe)
	} else {
		tui = exec.Command("script", "-qfec", "sh -c '"+strings.ReplaceAll(inner, `"$0"`, exe)+"'", "/dev/null")
	}
	tui.Dir, tui.Env = ws, env
	tui.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var out lockedBuf
	tui.Stdout, tui.Stderr = &out, &out
	stdin, _ := tui.StdinPipe()
	if err := tui.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stdin.Close(); syscall.Kill(-tui.Process.Pid, syscall.SIGKILL); tui.Wait() })
	wait := 20 * time.Second
	if !want {
		wait = 8 * time.Second
	}
	for deadline := time.Now().Add(wait); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		if want && strings.Contains(out.String(), "RESUME_MARKER") {
			break
		}
	}
	s := out.String()
	if !strings.Contains(s, "Casper") && !strings.Contains(s, "RESUME_MARKER") {
		t.Fatalf("the TUI drew nothing recognisable — the harness, not the behaviour, failed:\n%q", s)
	}
	return s
}
