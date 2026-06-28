package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// handleSlash dispatches the built-in slash commands: content commands render a
// transcript block, brief ones flash a snackbar, and /quit requests shutdown.
func TestHandleSlashDispatch(t *testing.T) {
	mm := newTestModel(t)
	m := &mm

	// /help renders an info block.
	before := len(m.blocks)
	if _, handled := m.handleSlash("/help"); !handled {
		t.Fatal("/help should be handled")
	}
	if len(m.blocks) != before+1 || m.blocks[len(m.blocks)-1].kind != blockInfo {
		t.Errorf("/help should append one info block, blocks=%d", len(m.blocks))
	}

	// /tools lists the registered tool names (bash is always present).
	m.handleSlash("/tools")
	last := m.blocks[len(m.blocks)-1]
	if last.kind != blockInfo || !strings.Contains(last.text, "bash") {
		t.Errorf("/tools should list tools incl. bash, got %q", last.text)
	}

	// An unknown command flashes a hint and appends no block.
	nblocks := len(m.blocks)
	m.handleSlash("/bogus")
	if !strings.Contains(m.snackbar, "unknown command") {
		t.Errorf("unknown-command snackbar = %q", m.snackbar)
	}
	if len(m.blocks) != nblocks {
		t.Error("unknown command must not append a block")
	}

	// /clear wipes the transcript.
	m.handleSlash("/clear")
	if len(m.blocks) != 0 {
		t.Errorf("/clear should empty blocks, got %d", len(m.blocks))
	}
	if !strings.Contains(m.snackbar, "cleared") {
		t.Errorf("/clear snackbar = %q", m.snackbar)
	}

	// /quit requests shutdown and returns a command.
	cmd, handled := m.handleSlash("/quit")
	if !handled || !m.quitting {
		t.Errorf("/quit should set quitting; handled=%v quitting=%v", handled, m.quitting)
	}
	if cmd == nil {
		t.Error("/quit should return a tea.Quit command")
	}
}

// Aliases route to their canonical command: /? -> help, /exit -> quit.
func TestHandleSlashAliases(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	if _, h := m.handleSlash("/?"); !h {
		t.Fatal("/? should be handled")
	}
	if m.blocks[len(m.blocks)-1].kind != blockInfo {
		t.Error("/? should render the help info block")
	}

	mm2 := newTestModel(t)
	m2 := &mm2
	if _, h := m2.handleSlash("/exit"); !h || !m2.quitting {
		t.Errorf("/exit should quit; handled=%v quitting=%v", h, m2.quitting)
	}
}

// expandMentions inlines the contents of @-mentioned files that exist in the
// workdir, deduping repeats and leaving unknown mentions untouched.
func TestExpandMentions(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	wd := t.TempDir()
	m.workdir = wd
	if err := os.WriteFile(filepath.Join(wd, "notes.txt"), []byte("alpha beta"), 0o644); err != nil {
		t.Fatal(err)
	}

	// An existing @file is appended under a header; the original text is kept.
	got := m.expandMentions("look at @notes.txt now")
	for _, want := range []string{"look at @notes.txt now", "--- notes.txt ---", "alpha beta"} {
		if !strings.Contains(got, want) {
			t.Errorf("expandMentions missing %q in:\n%s", want, got)
		}
	}

	// A missing file is left as-is (no append, no error).
	if got := m.expandMentions("@missing.txt"); got != "@missing.txt" {
		t.Errorf("missing mention should be unchanged, got %q", got)
	}

	// Duplicate mentions inline the file only once.
	got = m.expandMentions("@notes.txt and @notes.txt")
	if n := strings.Count(got, "--- notes.txt ---"); n != 1 {
		t.Errorf("duplicate mention should appear once, got %d:\n%s", n, got)
	}

	// No mention → identity.
	if got := m.expandMentions("plain text"); got != "plain text" {
		t.Errorf("no-mention identity broken: %q", got)
	}
}
