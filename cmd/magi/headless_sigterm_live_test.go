//go:build !windows

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A headless run (`magi -p`) stopped with SIGTERM while a tool is running must end the way a daemon
// does: through the bounded Shutdown, so the cut call is answered as "may have run", the turn is
// closed as interrupted, the tool's process group is gone and the exit says it did not finish.
// Before this, -p had no handler at all: the signal killed it outright (exit -15), the log stopped at
// tool.started and the `sleep` it had started was left running with nobody to end it.
func TestAHeadlessRunStoppedBySigtermClosesItsTurnAndLeavesNoChild(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the binary")
	}
	tmp := t.TempDir()
	bin := buildMagi(t, tmp)
	marker := filepath.Join(tmp, "marker")
	command := "echo ran >> " + marker + "; sleep 30"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"data":[{"id":"fixture","object":"model"}]}`)
			return
		}
		var req struct {
			Messages []struct {
				Role string `json:"role"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		answered := false
		for _, m := range req.Messages {
			if m.Role == "tool" {
				answered = true
			}
		}
		delta := map[string]any{"role": "assistant", "content": "done"}
		finish := "stop"
		if !answered {
			args, _ := json.Marshal(map[string]string{"command": command})
			delta = map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "c_bash1", "type": "function",
				"function": map[string]any{"name": "bash", "arguments": string(args)}}}}
			finish = "tool_calls"
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, ch := range []any{
			map[string]any{"choices": []any{map[string]any{"index": 0, "delta": delta}}},
			map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": finish}}, "usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 5}},
		} {
			b, _ := json.Marshal(ch)
			fmt.Fprintf(w, "data: %s\n\n", b)
		}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	work, cfg, data := filepath.Join(tmp, "w"), filepath.Join(tmp, "c"), filepath.Join(tmp, "d")
	for _, d := range []string{work, cfg, data} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(bin, "-p", "run the command")
	cmd.Dir = work
	cmd.Env = append(os.Environ(), "MAGI_CONFIG_DIR="+cfg, "MAGI_DATA_DIR="+data, "MAGI_BASE_URL="+server.URL+"/v1",
		"MAGI_MODEL=fixture", "MAGI_PERMISSION=allow", "MAGI_API_KEY=x", "OPENAI_API_KEY=", "MAGI_WORKFLOW=false",
		"MAGI_EMBEDDED_PLUGINS=off", "MAGI_SHUTDOWN_GRACE=10s")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); killMarkerGroups(marker) }()

	deadline := time.Now().Add(30 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the tool never started\nstderr:\n%s", stderr.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond) // into the sleep, not racing the echo
	// The tool's process group, taken while its shell still names the marker: once the shell is gone a
	// lone `sleep 30` no longer does, so the leftover check is by group, not by command line.
	groups := markerGroups(marker)
	if len(groups) == 0 {
		t.Fatalf("found no process group for the running tool")
	}
	defer func() {
		for _, g := range groups {
			_ = syscall.Kill(-g, syscall.SIGKILL)
		}
	}()

	sent := time.Now()
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatalf("still running 20s after SIGTERM (grace is 10s)\nstderr:\n%s", stderr.String())
	}
	took := time.Since(sent)

	ws, _ := cmd.ProcessState.Sys().(syscall.WaitStatus)
	if ws.Signaled() {
		t.Fatalf("SIGTERM killed the run outright (signal %v): nothing closed the turn or ended the tool\nstderr:\n%s", ws.Signal(), stderr.String())
	}
	if cmd.ProcessState.ExitCode() == 0 {
		t.Errorf("exit 0 for a run a signal cut short")
	}
	if took > 12*time.Second {
		t.Errorf("took %v to stop, past the 10s grace", took)
	}

	b, _ := os.ReadFile(marker)
	if n := strings.Count(string(b), "ran"); n != 1 {
		t.Errorf("the command ran %d times, want 1", n)
	}
	if left := groupMembers(groups); len(left) > 0 {
		t.Errorf("the tool's processes outlived the run: %v", left)
	}

	var types []string
	var result, errCode string
	_ = filepath.Walk(data, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ".jsonl") {
			return nil
		}
		f, err := os.Open(p)
		if err != nil {
			return nil
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 1<<24)
		var ts []string
		var res, code string
		for sc.Scan() {
			var e struct {
				Type string `json:"type"`
				Data struct {
					Code string `json:"code"`
					Part struct {
						ToolResult *struct {
							CallID  string `json:"callId"`
							Content string `json:"content"`
						} `json:"toolResult"`
					} `json:"part"`
				} `json:"data"`
			}
			if json.Unmarshal(sc.Bytes(), &e) != nil {
				continue
			}
			ts = append(ts, e.Type)
			if tr := e.Data.Part.ToolResult; tr != nil && tr.CallID == "c_bash1" {
				res = tr.Content
			}
			if e.Type == "error" {
				code = e.Data.Code
			}
		}
		for _, x := range ts {
			if x == "prompt.submitted" {
				types, result, errCode = ts, res, code
			}
		}
		return nil
	})
	if !strings.Contains(result, "may have run") {
		t.Errorf("the cut call's result = %q, want it to say it may have run\nevents: %v", result, types)
	}
	if errCode != "interrupted" {
		t.Errorf("error code = %q, want interrupted\nevents: %v", errCode, types)
	}
	if len(types) == 0 || types[len(types)-1] != "turn.finished" {
		t.Errorf("the turn was not closed: events %v", types)
	}
}

// markerProcesses lists processes whose command line names the marker — the tool's shell and
// whatever it is still sleeping in.
func markerProcesses(marker string) []string {
	out, _ := exec.Command("ps", "-axo", "pid=,pgid=,command=").Output()
	var hits []string
	for _, l := range strings.Split(string(out), "\n") {
		if strings.Contains(l, marker) {
			hits = append(hits, strings.TrimSpace(l))
		}
	}
	return hits
}

// markerGroups returns the process groups of the processes naming the marker.
func markerGroups(marker string) []int {
	seen := map[int]bool{}
	var gs []int
	for _, l := range markerProcesses(marker) {
		f := strings.Fields(l)
		var g int
		if len(f) >= 2 {
			if _, err := fmt.Sscan(f[1], &g); err == nil && g > 1 && !seen[g] {
				seen[g] = true
				gs = append(gs, g)
			}
		}
	}
	return gs
}

// groupMembers lists every live process in any of the groups.
func groupMembers(groups []int) []string {
	out, _ := exec.Command("ps", "-axo", "pid=,pgid=,command=").Output()
	var hits []string
	for _, l := range strings.Split(string(out), "\n") {
		f := strings.Fields(l)
		var g int
		if len(f) < 2 {
			continue
		}
		if _, err := fmt.Sscan(f[1], &g); err != nil {
			continue
		}
		for _, want := range groups {
			if g == want {
				hits = append(hits, strings.TrimSpace(l))
			}
		}
	}
	return hits
}

// killMarkerGroups ends what a failed run left behind, so a red test does not leave a sleep around.
func killMarkerGroups(marker string) {
	for _, l := range markerProcesses(marker) {
		f := strings.Fields(l)
		if len(f) < 2 {
			continue
		}
		var pgid int
		if _, err := fmt.Sscan(f[1], &pgid); err == nil && pgid > 1 {
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
		}
	}
}
