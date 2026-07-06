package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/sayaya1090/magi/internal/adapter/platform"
	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
)

// newShellModel builds a Model backed by the real OS platform so `!` commands run.
func newShellModel(t *testing.T) Model {
	t.Helper()
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := app.New(store, stubLLM{}, builtin.Default(), bus.New(), platform.New(), app.Config{Permission: "allow"})
	wd := t.TempDir()
	sid, err := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
	if err != nil {
		t.Fatal(err)
	}
	return New(context.Background(), a, nil, sid, "m", wd, true, "")
}

// A `!`-prefixed line runs a shell command: it renders a blockShell, stages the
// output, and clears the input — without starting an agent turn.
func TestShellBangStagesOutput(t *testing.T) {
	m := newShellModel(t)
	m.ta.SetValue("!echo hello")
	before := len(m.blocks)
	cmd, handled := m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !handled {
		t.Fatal("enter should handle a ! command")
	}
	if cmd == nil {
		t.Fatal("a ! command should return a cmd to run the shell off-loop")
	}
	// The command runs off the event loop and reports back via shellResultMsg — the
	// block is not appended until that message is processed.
	if len(m.blocks) != before {
		t.Fatal("block should not appear until the async result arrives")
	}
	msg := cmd()
	if _, ok := msg.(shellResultMsg); !ok {
		t.Fatalf("expected shellResultMsg, got %T", msg)
	}
	updated, _ := m.Update(msg)
	m = updated.(Model)
	if len(m.blocks) != before+1 || m.blocks[len(m.blocks)-1].kind != blockShell {
		t.Fatalf("expected a blockShell to be appended")
	}
	blk := m.blocks[len(m.blocks)-1]
	if blk.args != "echo hello" || !blk.ok || !strings.Contains(blk.text, "hello") {
		t.Fatalf("blockShell wrong: args=%q ok=%v text=%q", blk.args, blk.ok, blk.text)
	}
	if m.ta.Value() != "" {
		t.Fatalf("input should be cleared, got %q", m.ta.Value())
	}
	if m.running {
		t.Fatal("a ! command must not start a turn")
	}
	if len(m.pendingShell) != 1 {
		t.Fatalf("output should be staged, got %d pending", len(m.pendingShell))
	}

	// The block renders (no panic) with the command header and exit label.
	m.width, m.height, m.ready = 80, 24, true
	rendered := m.renderBlock(blk)
	if !strings.Contains(rendered, "echo hello") || !strings.Contains(rendered, "exit 0") {
		t.Fatalf("rendered block missing header/exit: %q", rendered)
	}

	// Draining folds the run into a prompt preamble the agent will read, then clears.
	pre := m.drainPendingShell()
	for _, want := range []string{"echo hello", "hello", "(exit 0)"} {
		if !strings.Contains(pre, want) {
			t.Fatalf("preamble %q missing %q", pre, want)
		}
	}
	if len(m.pendingShell) != 0 {
		t.Fatal("drain should clear the buffer")
	}
	if m.drainPendingShell() != "" {
		t.Fatal("second drain should be empty")
	}
}

// A bare "!" (no command) is ignored: no block, no stage.
func TestShellBangBareIgnored(t *testing.T) {
	m := newShellModel(t)
	m.ta.SetValue("!   ")
	before := len(m.blocks)
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(m.blocks) != before || len(m.pendingShell) != 0 {
		t.Fatal("bare ! should be a no-op")
	}
}

// stripControl removes escape/control sequences so untrusted output can't drive
// the terminal (audit finding N10), while keeping printable text and tabs.
func TestStripControl(t *testing.T) {
	in := "a\x1b[31mred\x1b[0m\x07\x00b\tc"
	got := stripControl(in)
	if want := "aredb\tc"; got != want {
		t.Fatalf("stripControl(%q) = %q, want %q", in, got, want)
	}
}

// clipLine is the single sanitization choke point for every tool-output body
// (bash/read/grep/glob/list/webfetch): control sequences must not survive it,
// even for a short line that skips the truncation path (audit finding N10).
func TestClipLineSanitizesControl(t *testing.T) {
	// OSC title-spoof + cursor-move + BEL embedded in otherwise-short content.
	in := "hi\x1b]0;pwned\x07\x1b[2Jthere"
	got := clipLine(in, 80)
	if strings.ContainsAny(got, "\x1b\x07") {
		t.Fatalf("clipLine left control sequences in %q", got)
	}
	if !strings.Contains(got, "hi") || !strings.Contains(got, "there") {
		t.Fatalf("clipLine dropped printable text: %q", got)
	}
}
