package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	task "github.com/sayaya1090/magi/internal/core/codingtask"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// This is a scripted provider, not a real-model quality claim. It exercises
// the built CLI, actual tool execution and the same-process workspace lease.
func TestCodingTaskLiveRunIsBoundToWorkspace(t *testing.T) {
	if testing.Short() {
		t.Skip("builds CLI")
	}
	binary := buildMagi(t, t.TempDir(), "-race")
	source := t.TempDir()
	store := filepath.Join(t.TempDir(), "tasks")
	cfg := t.TempDir()
	dataDir := t.TempDir()
	if e := os.WriteFile(filepath.Join(cfg, "config.toml"), []byte("[council]\nenabled = false\n"), 0600); e != nil {
		t.Fatal(e)
	}
	var calls, sidecars atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			fmt.Fprint(w, `{"data":[{"id":"fixture","context_length":32768}]}`)
			return
		}
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		var request struct {
			Tools    []json.RawMessage `json:"tools"`
			Messages []struct {
				Role       string          `json:"role"`
				Content    json.RawMessage `json:"content"`
				ToolCallID string          `json:"tool_call_id"`
			} `json:"messages"`
		}
		if e := json.NewDecoder(r.Body).Decode(&request); e != nil {
			t.Error(e)
			http.Error(w, "invalid request", 400)
			return
		}
		// Engram may issue one post-turn analysis request. It is not a
		// second foreground answer and must not enter the conversation log.
		if len(request.Tools) == 0 && len(request.Messages) > 0 && strings.Contains(string(request.Messages[0].Content), "개발 대화 로그를 분석하는 전용 분석기") {
			sidecars.Add(1)
			w.Header().Set("Content-Type", "text/event-stream")
			chunk := map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": `{"lesson":null,"skill":null}`}, "finish_reason": "stop"}}}
			b, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", b)
			return
		}
		var system string
		for _, m := range request.Messages {
			if m.Role == "system" {
				system += string(m.Content)
			}
		}
		if !strings.Contains(system, "ALREADY inside the prepared coding-task workspace") || strings.Contains(system, "MUST call coding_task") {
			t.Errorf("trusted runner binding did not reach the actual model request: %s", system)
		}
		n := calls.Add(1)
		if n == 2 {
			results := 0
			for _, message := range request.Messages {
				if message.Role == "tool" && message.ToolCallID == "write-one" {
					results++
				}
			}
			if results != 1 {
				t.Errorf("second request has %d matching tool results", results)
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 1 {
			chunk := map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "write-one", "type": "function", "function": map[string]any{"name": "write", "arguments": `{"path":"generated.txt","content":"isolated"}`}}}}, "finish_reason": "tool_calls"}}}
			b, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", b)
		} else {
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Complete\"},\"finish_reason\":\"stop\"}]}\n\n")
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	common := []string{"--source", source, "--store", store}
	args := append([]string{"coding-task", "prepare", "--mode", "copy"}, common...)
	b, e := exec.Command(binary, args...).CombinedOutput()
	if e != nil {
		t.Fatalf("prepare: %v %s", e, b)
	}
	var record task.Record
	if e = json.Unmarshal(b, &record); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	args = append([]string{"coding-task", "run", "--id", record.ID, "--timeout", "30s", "--prompt", "Write generated.txt containing isolated, then finish."}, common...)
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = append(os.Environ(), "MAGI_CONFIG_DIR="+cfg, "MAGI_DATA_DIR="+dataDir, "MAGI_BASE_URL="+server.URL+"/v1", "MAGI_MODEL=fixture", "MAGI_PERMISSION=allow", "MAGI_API_KEY=", "OPENAI_API_KEY=", "MAGI_WORKFLOW=false")
	b, e = cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("run: %v %s", e, b)
	}
	if _, e = os.Stat(filepath.Join(source, "generated.txt")); !os.IsNotExist(e) {
		t.Fatal("model wrote source")
	}
	result, e := os.ReadFile(filepath.Join(record.Workspace, "generated.txt"))
	if e != nil || string(result) != "isolated" {
		t.Fatalf("workspace result %q: %v\n%s", result, e, b)
	}
	auditCodingTaskEvents(t, dataDir, record.Workspace, true)
	if sidecars.Load() > 1 {
		t.Errorf("duplicate background analysis: %d", sidecars.Load())
	}
	t.Logf("foreground requests=%d, background analyses=%d", calls.Load(), sidecars.Load())
	if calls.Load() != 2 {
		t.Fatalf("expected 2 provider requests, got %d; output: %s", calls.Load(), b)
	}
}

func TestCodingTaskLiveCancellationPreservesWorkspace(t *testing.T) {
	if testing.Short() {
		t.Skip("builds CLI")
	}
	binary := buildMagi(t, t.TempDir(), "-race")
	source := t.TempDir()
	store := filepath.Join(t.TempDir(), "tasks")
	cfg := t.TempDir()
	dataDir := t.TempDir()
	if e := os.WriteFile(filepath.Join(cfg, "config.toml"), []byte("[council]\nenabled = false\n"), 0600); e != nil {
		t.Fatal(e)
	}
	closed := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			fmt.Fprint(w, `{"data":[{"id":"fixture","context_length":32768}]}`)
			return
		}
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-closed:
		}
	}))
	defer func() { close(closed); server.Close() }()
	common := []string{"--source", source, "--store", store}
	b, e := exec.Command(binary, append([]string{"coding-task", "prepare", "--mode", "copy"}, common...)...).CombinedOutput()
	if e != nil {
		t.Fatalf("%v %s", e, b)
	}
	var record task.Record
	if e = json.Unmarshal(b, &record); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	args := append([]string{"coding-task", "run", "--id", record.ID, "--timeout", "1s", "--prompt", "Wait for a response."}, common...)
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = append(os.Environ(), "MAGI_CONFIG_DIR="+cfg, "MAGI_DATA_DIR="+dataDir, "MAGI_BASE_URL="+server.URL+"/v1", "MAGI_MODEL=fixture", "MAGI_PERMISSION=allow", "MAGI_API_KEY=", "OPENAI_API_KEY=", "MAGI_WORKFLOW=false")
	b, e = cmd.CombinedOutput()
	if e == nil || ctx.Err() != nil {
		t.Fatalf("run error=%v outer context=%v\n%s", e, ctx.Err(), b)
	}
	b, e = exec.Command(binary, append([]string{"coding-task", "status", "--id", record.ID}, common...)...).CombinedOutput()
	if e != nil {
		t.Fatalf("%v %s", e, b)
	}
	var status task.Summary
	if e = json.Unmarshal(b, &status); e != nil {
		t.Fatal(e)
	}
	if status.State != "stopped" {
		t.Fatalf("%+v", status)
	}
	auditCodingTaskEvents(t, dataDir, record.Workspace, false)
	if _, e = os.Stat(record.Workspace); e != nil {
		t.Fatal("cancel deleted workspace", e)
	}
}

// Audit durable events after the runner exits, not just the final file. This
// catches duplicate completion, mismatched tool results and session/workdir drift.
func auditCodingTaskEvents(t *testing.T, dataDir, workspace string, success bool) {
	t.Helper()
	logs := 0
	err := filepath.WalkDir(filepath.Join(dataDir, "projects"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		logs++
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 4096), 1<<20)
		var seq int64
		var sid session.SessionID
		created, prompts, finishes, texts, toolCalls, toolResults := 0, 0, 0, 0, 0, 0
		pending := map[string]bool{}
		for scanner.Scan() {
			var ev event.Event
			if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
				return err
			}
			if ev.Seq <= seq {
				t.Errorf("event sequence did not increase: %d after %d", ev.Seq, seq)
			}
			seq = ev.Seq
			if sid == "" {
				sid = ev.SessionID
			}
			if ev.SessionID != sid {
				t.Error("session identity changed within log")
			}
			if finishes > 0 && (ev.Type == event.TypePartAppended || ev.Type == event.TypePromptSubmitted) {
				t.Errorf("conversation event after turn completion: %s", ev.Type)
			}
			switch ev.Type {
			case event.TypeSessionCreated:
				created++
				var d event.SessionCreatedData
				if err := json.Unmarshal(ev.Data, &d); err != nil {
					return err
				}
				if d.Workdir != workspace {
					t.Errorf("session workspace %q, expected %q", d.Workdir, workspace)
				}
			case event.TypePromptSubmitted:
				prompts++
			case event.TypeTurnFinished:
				finishes++
			case event.TypeError:
				if success {
					t.Errorf("unexpected error event: %s", ev.Data)
				}
			case event.TypePartAppended:
				var d event.PartAppendedData
				if err := json.Unmarshal(ev.Data, &d); err != nil {
					return err
				}
				switch d.Part.Kind {
				case session.PartToolCall:
					toolCalls++
					if d.Part.ToolCall == nil {
						t.Error("missing tool call")
						break
					}
					id := d.Part.ToolCall.CallID
					if pending[id] {
						t.Error("duplicate tool call", id)
					}
					pending[id] = true
				case session.PartToolResult:
					toolResults++
					if d.Part.ToolResult == nil {
						t.Error("missing tool result")
						break
					}
					id := d.Part.ToolResult.CallID
					if !pending[id] {
						t.Error("tool result without pending call", id)
					}
					delete(pending, id)
					if success && d.Part.ToolResult.IsError {
						t.Error("tool failed during successful turn")
					}
				case session.PartText:
					if d.Role == session.RoleAssistant {
						texts++
					}
				}
			}
		}
		if created != 1 || prompts != 1 || finishes > 1 {
			t.Errorf("created=%d prompts=%d finishes=%d", created, prompts, finishes)
		}
		if success && (finishes != 1 || toolCalls != 1 || toolResults != 1 || texts != 1 || len(pending) != 0) {
			t.Errorf("turn events: finishes=%d calls=%d results=%d assistantTexts=%d pending=%d", finishes, toolCalls, toolResults, texts, len(pending))
		}
		if !success && (toolCalls != 0 || toolResults != 0 || texts != 0) {
			t.Errorf("cancelled silent backend produced content: calls=%d results=%d texts=%d", toolCalls, toolResults, texts)
		}
		return scanner.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	if logs != 1 {
		t.Errorf("expected one task session log, got %d", logs)
	}
}

func TestCodingTaskLiveConcurrentProcessesShareOneTask(t *testing.T) {
	if testing.Short() {
		t.Skip("builds CLI")
	}
	binary := buildMagi(t, t.TempDir(), "-race")
	source, store := t.TempDir(), filepath.Join(t.TempDir(), "tasks")
	if e := os.WriteFile(filepath.Join(source, "original.txt"), []byte("source"), 0600); e != nil {
		t.Fatal(e)
	}
	type result struct {
		output []byte
		err    error
	}
	start := make(chan struct{})
	done := make(chan result, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for range 2 {
		go func() {
			<-start
			output, err := exec.CommandContext(ctx, binary, "coding-task", "prepare", "--source", source, "--store", store, "--mode", "copy", "--request", "cross-process").CombinedOutput()
			done <- result{output, err}
		}()
	}
	close(start)
	first, second := <-done, <-done
	if first.err != nil || second.err != nil {
		t.Fatalf("concurrent prepare: %v %s; %v %s", first.err, first.output, second.err, second.output)
	}
	var a, b task.Summary
	if e := json.Unmarshal(first.output, &a); e != nil {
		t.Fatal(e)
	}
	if e := json.Unmarshal(second.output, &b); e != nil {
		t.Fatal(e)
	}
	if a.ID == "" || a.ID != b.ID || a.Workspace != b.Workspace || a.State != "ready" || b.State != "ready" {
		t.Fatalf("duplicate or incomplete workspaces: %+v %+v", a, b)
	}
	entries, e := os.ReadDir(filepath.Join(store, "tasks"))
	if e != nil || len(entries) != 1 {
		t.Fatalf("task count %d: %v", len(entries), e)
	}
	original, e := os.ReadFile(filepath.Join(source, "original.txt"))
	if e != nil || string(original) != "source" {
		t.Fatalf("source changed: %q %v", original, e)
	}
}
