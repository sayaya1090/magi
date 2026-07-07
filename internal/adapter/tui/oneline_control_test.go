package tui

import (
	"strings"
	"testing"
)

// oneLine feeds the transcript's one-line preview/header paths (collapsed thoughts,
// tool-arg/result summaries, subagent snippets) with model-generated, untrusted
// content. Those paths skip clipLine, so oneLine must itself strip terminal control
// sequences — otherwise an embedded OSC/CSI/BEL can spoof the title, move the cursor,
// or clear the screen from a preview line (audit finding N10, sibling of the body
// path already guarded by clipLine).
func TestOneLineStripsControl(t *testing.T) {
	// OSC title-spoof + BEL + CSI clear-screen + NUL embedded in short text.
	in := "before\x1b]0;PWNED\x07\x1b[2Jafter\x00end"
	got := oneLine(in, 200)
	if strings.ContainsAny(got, "\x1b\x07\x00") {
		t.Fatalf("oneLine left control bytes: %q", got)
	}
	// Printable content survives (concatenated once control is stripped).
	for _, want := range []string{"before", "after", "end"} {
		if !strings.Contains(got, want) {
			t.Fatalf("oneLine dropped printable %q: got %q", want, got)
		}
	}
}

// Newlines must become spaces (not vanish): stripControl drops \n outright, so the
// replace-then-strip ordering matters — otherwise adjacent lines fuse into one word.
func TestOneLineNewlineBecomesSpace(t *testing.T) {
	if got := oneLine("line1\nline2", 200); got != "line1 line2" {
		t.Fatalf("oneLine(newline) = %q, want %q", got, "line1 line2")
	}
}

// The collapsed reasoning preview is a real render path built via oneLine on model
// "thinking" text; a control sequence in that text must not survive into the frame.
func TestReasoningPreviewSanitized(t *testing.T) {
	m := Model{width: 80, height: 24, ready: true} // showThink zero-value false → collapsed preview
	blk := block{kind: blockReasoning, text: "peek\x1b]0;PWNED\x07\x1b[2Jaboo\x00"}
	rendered := m.renderBlock(blk)
	if strings.ContainsAny(rendered, "\x07\x00") || strings.Contains(rendered, "\x1b]0;") || strings.Contains(rendered, "\x1b[2J") {
		t.Fatalf("reasoning preview leaked control sequence: %q", rendered)
	}
	if !strings.Contains(rendered, "peek") || !strings.Contains(rendered, "aboo") {
		t.Fatalf("reasoning preview dropped printable text: %q", rendered)
	}
}
