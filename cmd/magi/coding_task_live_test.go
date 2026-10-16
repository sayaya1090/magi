package main

import (
	"context"
	"encoding/json"
	"fmt"
	task "github.com/sayaya1090/magi/internal/core/codingtask"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
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
	binary := buildMagi(t, t.TempDir())
	source := t.TempDir()
	store := filepath.Join(t.TempDir(), "tasks")
	cfg := t.TempDir()
	if e := os.WriteFile(filepath.Join(cfg, "config.toml"), []byte("[council]\nenabled = false\n"), 0600); e != nil {
		t.Fatal(e)
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			fmt.Fprint(w, `{"data":[{"id":"fixture","context_length":32768}]}`)
			return
		}
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if calls.Add(1) == 1 {
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
	cmd.Env = append(os.Environ(), "MAGI_CONFIG_DIR="+cfg, "MAGI_DATA_DIR="+t.TempDir(), "MAGI_BASE_URL="+server.URL+"/v1", "MAGI_MODEL=fixture", "MAGI_PERMISSION=allow", "MAGI_API_KEY=", "OPENAI_API_KEY=", "MAGI_WORKFLOW=false")
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
	if calls.Load() < 2 {
		t.Fatal("tool loop not completed")
	}
}

func TestCodingTaskLiveCancellationPreservesWorkspace(t *testing.T) {
	if testing.Short() {
		t.Skip("builds CLI")
	}
	binary := buildMagi(t, t.TempDir())
	source := t.TempDir()
	store := filepath.Join(t.TempDir(), "tasks")
	cfg := t.TempDir()
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
	cmd.Env = append(os.Environ(), "MAGI_CONFIG_DIR="+cfg, "MAGI_DATA_DIR="+t.TempDir(), "MAGI_BASE_URL="+server.URL+"/v1", "MAGI_MODEL=fixture", "MAGI_PERMISSION=allow", "MAGI_API_KEY=", "OPENAI_API_KEY=", "MAGI_WORKFLOW=false")
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
	if _, e = os.Stat(record.Workspace); e != nil {
		t.Fatal("cancel deleted workspace", e)
	}
}
