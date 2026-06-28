package builtin

import (
	"context"
	"encoding/json"
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

func TestSyncBufferReadSince(t *testing.T) {
	b := &syncBuffer{}
	b.Write([]byte("hello "))
	out, next, dropped := b.readSince(0)
	if out != "hello " || next != 6 || dropped {
		t.Fatalf("first read = %q,%d,%v", out, next, dropped)
	}
	b.Write([]byte("world"))
	out, next, dropped = b.readSince(next) // only the new bytes
	if out != "world" || next != 11 || dropped {
		t.Fatalf("incremental read = %q,%d,%v", out, next, dropped)
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
