package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// bash_input drives an interactive process: send a line to `cat`'s stdin and see
// it echoed back via bash_output. Then eof closes stdin so cat exits.
func TestBackgroundInput(t *testing.T) {
	env := port.ToolEnv{Workdir: t.TempDir()}
	start, _ := Bash{}.Execute(context.Background(),
		json.RawMessage(`{"command":"cat","background":true}`), env)
	id := bgIDRE.FindString(resultText(t, start))
	if id == "" {
		t.Fatal("no background id")
	}

	in, _ := BashInput{}.Execute(context.Background(),
		json.RawMessage(`{"id":"`+id+`","input":"hello","newline":true}`), env)
	if in.IsError {
		t.Fatalf("input errored: %s", resultText(t, in))
	}

	idArg := json.RawMessage(`{"id":"` + id + `"}`)
	var acc string
	for i := 0; i < 100; i++ { // cat echoes the line back
		r, _ := BashOutput{}.Execute(context.Background(), idArg, env)
		acc += resultText(t, r)
		if strings.Contains(acc, "hello") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(acc, "hello") {
		t.Errorf("no echo of stdin, got %q", acc)
	}

	// Unknown id is a clean error.
	e, _ := BashInput{}.Execute(context.Background(),
		json.RawMessage(`{"id":"bg_999999","input":"x"}`), env)
	if !e.IsError {
		t.Error("input to unknown id should error")
	}

	// eof closes stdin → cat exits.
	if eof, _ := (BashInput{}).Execute(context.Background(),
		json.RawMessage(`{"id":"`+id+`","eof":true}`), env); eof.IsError {
		t.Fatalf("eof errored: %s", resultText(t, eof))
	}
	exited := false
	for i := 0; i < 100; i++ {
		r, _ := BashOutput{}.Execute(context.Background(), idArg, env)
		if strings.Contains(resultText(t, r), "exited") {
			exited = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !exited {
		t.Error("cat should exit after stdin EOF")
	}
}

// Sending input to a process that has already exited is a clean error, not a panic.
func TestBackgroundInputAfterExit(t *testing.T) {
	env := port.ToolEnv{Workdir: t.TempDir()}
	start, _ := Bash{}.Execute(context.Background(),
		json.RawMessage(`{"command":"true","background":true}`), env)
	id := bgIDRE.FindString(resultText(t, start))
	idArg := json.RawMessage(`{"id":"` + id + `"}`)
	for i := 0; i < 100; i++ { // wait for exit
		r, _ := BashOutput{}.Execute(context.Background(), idArg, env)
		if strings.Contains(resultText(t, r), "exited") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	e, _ := BashInput{}.Execute(context.Background(),
		json.RawMessage(`{"id":"`+id+`","input":"x"}`), env)
	if !e.IsError {
		t.Error("input to an exited process should error")
	}
}

func TestReadLogSince(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log")
	if err := os.WriteFile(path, []byte("hello "), 0o644); err != nil {
		t.Fatal(err)
	}
	out, next := readLogSince(path, 0)
	if out != "hello " || next != 6 {
		t.Fatalf("first read = %q,%d", out, next)
	}
	// Append and read only the new bytes from the prior offset.
	if err := os.WriteFile(path, []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, next = readLogSince(path, next)
	if out != "world" || next != 11 {
		t.Fatalf("incremental read = %q,%d", out, next)
	}
	// Reading a missing file is a clean empty read, not a panic.
	if out, next := readLogSince(filepath.Join(t.TempDir(), "nope"), 5); out != "" || next != 5 {
		t.Fatalf("missing-file read = %q,%d", out, next)
	}
}

func resultText(t *testing.T, r session.ToolResult) string {
	t.Helper()
	var s string
	if err := json.Unmarshal(r.Content, &s); err != nil {
		return string(r.Content)
	}
	return s
}

var bgIDRE = regexp.MustCompile(`bg_\d+`)

func TestBackgroundLifecycle(t *testing.T) {
	env := port.ToolEnv{Workdir: t.TempDir()}
	// Start a quick background command that prints two lines and exits.
	start, _ := Bash{}.Execute(context.Background(),
		json.RawMessage(`{"command":"printf 'a\nb\n'","background":true}`), env)
	if start.IsError {
		t.Fatalf("start failed: %s", resultText(t, start))
	}
	id := bgIDRE.FindString(resultText(t, start))
	if id == "" {
		t.Fatalf("no id in: %s", resultText(t, start))
	}

	idArg := json.RawMessage(`{"id":"` + id + `"}`)
	var acc string
	for i := 0; i < 100; i++ {
		r, _ := BashOutput{}.Execute(context.Background(), idArg, env)
		acc += resultText(t, r)
		if strings.Contains(acc, "exited") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(acc, "a") || !strings.Contains(acc, "b") {
		t.Errorf("missing background output, got: %q", acc)
	}
	if !strings.Contains(acc, "exited") {
		t.Errorf("process never reported exit: %q", acc)
	}
}

func TestBackgroundKill(t *testing.T) {
	env := port.ToolEnv{Workdir: t.TempDir()}
	start, _ := Bash{}.Execute(context.Background(),
		json.RawMessage(`{"command":"sleep 30","background":true}`), env)
	id := bgIDRE.FindString(resultText(t, start))
	if id == "" {
		t.Fatal("no id")
	}
	kill, _ := BashKill{}.Execute(context.Background(), json.RawMessage(`{"id":"`+id+`"}`), env)
	if kill.IsError || !strings.Contains(resultText(t, kill), "killed") {
		t.Errorf("kill = %s", resultText(t, kill))
	}
	// Unknown id is a clean error, not a panic.
	r, _ := (BashKill{}).Execute(context.Background(), json.RawMessage(`{"id":"bg_999999"}`), env)
	if !r.IsError {
		t.Error("killing an unknown id should error")
	}
}

// TestBackgroundKillStatusRace guards N11: a bash_output issued *immediately*
// after bash_kill — before the reaper goroutine has observed the process exit —
// must report "[id killed]", never a stale "[id running]". The kill path sets
// p.killed synchronously (bgproc.go) precisely so this window can't lie.
func TestBackgroundKillStatusRace(t *testing.T) {
	env := port.ToolEnv{Workdir: t.TempDir()}
	start, _ := Bash{}.Execute(context.Background(),
		json.RawMessage(`{"command":"sleep 30","background":true}`), env)
	id := bgIDRE.FindString(resultText(t, start))
	if id == "" {
		t.Fatal("no id")
	}
	idArg := json.RawMessage(`{"id":"` + id + `"}`)
	kill, _ := BashKill{}.Execute(context.Background(), idArg, env)
	if kill.IsError {
		t.Fatalf("kill errored: %s", resultText(t, kill))
	}
	// No sleep: read status in the same instant the kill returns. The reaper may
	// or may not have observed the exit yet — either way the status must be a
	// terminal one ("killed" if we beat the reaper, "exited"/"done" if it beat us).
	// The one thing it must never be is the stale "running" the N11 fix eliminates.
	out, _ := BashOutput{}.Execute(context.Background(), idArg, env)
	got := resultText(t, out)
	if strings.Contains(got, "running") {
		t.Errorf("bash_output right after kill reported a stale 'running' status: %q", got)
	}
	if !strings.Contains(got, "killed") && !strings.Contains(got, "done") && !strings.Contains(got, "exited") {
		t.Errorf("bash_output after kill should be a terminal status, got %q", got)
	}
}
