//go:build !windows

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/shortdir"
)

// A real daemon, a real SIGTERM while a tool runs, a real restart. Measured end to end:
//   - the daemon exits within the grace, and the tool's whole process group is gone (by group, not
//     by command line: a lone `sleep` outliving its shell no longer names the marker);
//   - the cut call is answered "may have run", marked MayHaveRun, and the turn closed as interrupted
//     and not resumable — so the restart finds nothing for recovery to write;
//   - nothing re-runs it: only when the person says "continue" and the model asks again does the
//     command run a second time, as a NEW call the model made having been told it may have run.
func TestADaemonStoppedBySigtermHandsTheCutCallToThePersonAndRestartsClean(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the binary")
	}
	cfg, err := shortdir.Make("mgs")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(cfg) })
	ws, err := shortdir.Make("mgt")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(ws) })
	data := t.TempDir()
	bin := buildMagi(t, t.TempDir())
	marker := filepath.Join(ws, "marker")
	commandText := "echo ran >> " + marker + "; if [ $(grep -c ran " + marker + ") -lt 2 ]; then sleep 30; fi"

	var mu sync.Mutex
	var sawMayHaveRun, reissued bool
	var seen []string // each request the fake model got: its roles in order, and the last user words
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"data":[{"id":"fixture","object":"model"}]}`)
			return
		}
		var req struct {
			Messages []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		tools, lastUser, mayHaveRun, continued := 0, "", false, false
		roles := make([]string, 0, len(req.Messages))
		for _, m := range req.Messages {
			roles = append(roles, m.Role)
			switch m.Role {
			case "tool":
				tools++
				if strings.Contains(string(m.Content), "may have run") {
					mayHaveRun = true
				}
			case "user":
				lastUser = string(m.Content)
				// magi appends its own runtime-context note after the person's words, so "continue"
				// is any user message after the tool answer, not necessarily the last one.
				if tools > 0 && strings.Contains(lastUser, "continue") {
					continued = true
				}
			}
		}
		mu.Lock()
		seen = append(seen, fmt.Sprintf("roles=%v lastUser=%.60s", roles, lastUser))
		mu.Unlock()
		delta := map[string]any{"role": "assistant", "content": "done"}
		finish := "stop"
		callID := ""
		switch {
		case tools == 0:
			callID = "c_bash1"
		case tools == 1 && continued:
			callID = "c_bash2" // the model asks for the same command again
			mu.Lock()
			sawMayHaveRun, reissued = mayHaveRun, true
			mu.Unlock()
		}
		if callID != "" {
			args, _ := json.Marshal(map[string]string{"command": commandText})
			delta = map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": callID, "type": "function",
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
	// Closed after the daemons are killed (cleanups run last-registered first): httptest waits on
	// open connections, and a live daemon holding one would hang the test here.
	t.Cleanup(server.Close)

	env := append(os.Environ(), "MAGI_CONFIG_DIR="+cfg, "MAGI_DATA_DIR="+data, "MAGI_BASE_URL="+server.URL+"/v1",
		"MAGI_MODEL=fixture", "MAGI_PERMISSION=allow", "MAGI_API_KEY=x", "OPENAI_API_KEY=", "MAGI_WORKFLOW=false",
		"MAGI_EMBEDDED_PLUGINS=off", "MAGI_SHUTDOWN_GRACE=10s")
	sock := daemon.SocketPath(cfg, ws)
	start := func() (*exec.Cmd, *strings.Builder, *daemon.Client) {
		cmd := exec.Command(bin, "--daemon")
		cmd.Dir, cmd.Env = ws, env
		var stderr strings.Builder
		cmd.Stderr = &stderr
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cmd.Process.Kill() })
		deadline := time.Now().Add(30 * time.Second)
		for {
			if c, err := daemon.Dial(sock); err == nil {
				return cmd, &stderr, c
			}
			if time.Now().After(deadline) {
				t.Fatalf("the daemon never answered on %s\nstderr:\n%s", sock, stderr.String())
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	// Bounded: a daemon that never answers a submit dumps its goroutines (SIGQUIT) into the failure,
	// so a hang names where it hangs instead of eating the package timeout.
	submit := func(d *exec.Cmd, stderr *strings.Builder, c *daemon.Client, sid, text string) {
		errc := make(chan error, 1)
		go func() {
			errc <- c.Submit(context.Background(), command.SubmitPrompt{SessionID: session.SessionID(sid),
				Parts: []session.Part{{Kind: session.PartText, Text: text}}})
		}()
		select {
		case err := <-errc:
			if err != nil {
				t.Fatalf("submit %q: %v", text, err)
			}
		case <-time.After(15 * time.Second):
			_ = d.Process.Signal(syscall.SIGQUIT)
			time.Sleep(2 * time.Second)
			t.Fatalf("the daemon did not answer submit %q in 15s\nstderr:\n%s", text, stderr.String())
		}
	}

	d1, stderr1, c1 := start()
	sid, err := c1.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	submit(d1, stderr1, c1, sid, "run the command")
	deadline := time.Now().Add(30 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the tool never started\nstderr:\n%s", stderr1.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
	groups := markerGroups(marker)
	if len(groups) == 0 {
		t.Fatal("found no process group for the running tool")
	}
	defer func() {
		for _, g := range groups {
			_ = syscall.Kill(-g, syscall.SIGKILL)
		}
	}()
	c1.Close()

	sent := time.Now()
	if err := d1.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- d1.Wait() }()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatalf("still running 20s after SIGTERM\nstderr:\n%s", stderr1.String())
	}
	took := time.Since(sent)
	t.Logf("SIGTERM → exit %d in %v", d1.ProcessState.ExitCode(), took.Round(time.Millisecond))
	if took > 12*time.Second {
		t.Errorf("took %v to stop, past the 10s grace", took)
	}
	if d1.ProcessState.ExitCode() != 0 {
		t.Errorf("a daemon stopped by SIGTERM should exit 0, got %d\nstderr:\n%s", d1.ProcessState.ExitCode(), stderr1.String())
	}
	if left := groupMembers(groups); len(left) > 0 {
		t.Errorf("the tool's processes outlived the daemon: %v", left)
	}

	d2, stderr2, c2 := start()
	defer c2.Close()
	// History holds its connection as a transcript stream (daemon.Client.History: "its own
	// connection"), so each read dials its own and the submits keep c2.
	history := func() []event.Event {
		c, err := daemon.Dial(sock)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		evs, err := c.History(sid)
		if err != nil {
			t.Fatal(err)
		}
		return evs
	}
	evs := history()
	var r1 *session.ToolResult
	var interrupted *event.ErrorData
	for _, e := range evs {
		if e.Actor.ID == "recovery" {
			t.Errorf("recovery wrote %s on a turn the shutdown had already closed", e.Type)
		}
		switch e.Type {
		case event.TypePartAppended:
			var d event.PartAppendedData
			if json.Unmarshal(e.Data, &d) == nil && d.Part.ToolResult != nil && d.Part.ToolResult.CallID == "c_bash1" {
				r1 = d.Part.ToolResult
			}
		case event.TypeError:
			var d event.ErrorData
			if json.Unmarshal(e.Data, &d) == nil && d.Code == "interrupted" {
				interrupted = &d
			}
		}
	}
	if r1 == nil || !r1.MayHaveRun || !strings.Contains(string(r1.Content), "may have run") {
		t.Fatalf("c_bash1 should be answered may-have-run and marked so; got %+v", r1)
	}
	if interrupted == nil || interrupted.Resumable {
		t.Fatalf("want an interruption that is not resumable; got %+v", interrupted)
	}
	if b, _ := os.ReadFile(marker); strings.Count(string(b), "ran") != 1 {
		t.Fatalf("before anyone said continue the command ran %d times, want 1", strings.Count(string(b), "ran"))
	}

	// R2: the person says continue and the model asks for the same command again.
	submit(d2, stderr2, c2, sid, "continue")
	deadline = time.Now().Add(30 * time.Second)
	for {
		evs = history()
		finished := 0
		for _, e := range evs {
			if e.Type == event.TypeTurnFinished {
				finished++
			}
		}
		if finished >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the continued turn never finished\nstderr:\n%s", stderr2.String())
		}
		time.Sleep(100 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	ran := 0
	if b, err := os.ReadFile(marker); err == nil {
		ran = strings.Count(string(b), "ran")
	}
	for i, r := range seen {
		t.Logf("model request %d: %s", i+1, r)
	}
	t.Logf("R2: model re-asked=%v, its request carried the may-have-run answer=%v, command runs=%d", reissued, sawMayHaveRun, ran)
	if !reissued || !sawMayHaveRun {
		t.Errorf("the model must see the may-have-run answer before it asks again (re-asked=%v saw=%v)", reissued, sawMayHaveRun)
	}
	if ran != 2 {
		t.Errorf("want exactly one run per call (2 calls), got %d", ran)
	}
}
