//go:build !windows

package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/port"
)

// A pty:true background process gets a real controlling terminal, so a program that reads
// from /dev/tty (the thing ssh's password prompt and a serial getty login do) sees input
// sent with bash_input. The pipe path has no controlling tty, so this same read would fail —
// this is exactly the qemu-alpine-ssh gap the pty option closes.
func TestBackgroundPTYControllingTTY(t *testing.T) {
	env := port.ToolEnv{Workdir: t.TempDir()}
	// Read one line from the CONTROLLING TERMINAL (not stdin) and echo it back.
	start, _ := Bash{}.Execute(context.Background(),
		json.RawMessage(`{"command":"sh -c 'IFS= read -r line < /dev/tty; echo GOT:$line'","background":true,"pty":true}`), env)
	txt := resultText(t, start)
	id := bgIDRE.FindString(txt)
	if id == "" {
		t.Fatalf("no background id (pty start failed?): %s", txt)
	}
	if !strings.Contains(txt, "pseudo-terminal") {
		t.Errorf("start message should note the pty: %s", txt)
	}

	// Drive the controlling tty: this only reaches /dev/tty because a pty is attached.
	in, _ := BashInput{}.Execute(context.Background(),
		json.RawMessage(`{"id":"`+id+`","input":"hello","newline":true}`), env)
	if in.IsError {
		t.Fatalf("input errored: %s", resultText(t, in))
	}

	idArg := json.RawMessage(`{"id":"` + id + `"}`)
	var acc string
	for i := 0; i < 150; i++ {
		r, _ := BashOutput{}.Execute(context.Background(), idArg, env)
		acc += resultText(t, r)
		if strings.Contains(acc, "GOT:hello") {
			return // success: the tty read saw our input
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("program never read our input from /dev/tty; output so far: %q", acc)
}

// ptyNeededNote steers a non-pty background of a tty-gated command; with pty it does not fire
// (and the process is actually driven — covered above). Here: the note appears when a serial
// console qemu is backgrounded on a plain pipe.
func TestBackgroundPTYNoteOnPipe(t *testing.T) {
	env := port.ToolEnv{Workdir: t.TempDir()}
	// Not really qemu — a harmless stand-in whose command text trips ptyGated; it exits
	// immediately, we only assert the steer note rode along on the start message.
	start, _ := Bash{}.Execute(context.Background(),
		json.RawMessage(`{"command":"echo qemu-system-x86_64 -nographic","background":true}`), env)
	txt := resultText(t, start)
	if !strings.Contains(txt, "controlling terminal") || !strings.Contains(txt, "pty:true") {
		t.Errorf("a tty-gated command backgrounded without pty should be steered to pty:true, got: %s", txt)
	}
}
