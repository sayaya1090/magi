package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// sliceLines counts the way every editor counts: 1-indexed, inclusive, clamped at the end, and a
// malformed range falls back to the whole file — the person pointed at the FILE, and losing it
// over a bad range drops the half that mattered.
func TestSliceLinesCountsLikeAnEditor(t *testing.T) {
	c := "one\ntwo\nthree\nfour"
	for lines, want := range map[string]string{
		"2-3":  "two\nthree",
		"2":    "two",
		"3-99": "three\nfour",
		"":     c,
		"x-y":  c,
		"3-2":  c,
	} {
		if got := sliceLines(c, lines); got != want {
			t.Errorf("sliceLines(%q) = %q, want %q", lines, got, want)
		}
	}
	if got := sliceLines(c, "9-12"); !strings.Contains(got, "4 lines") {
		t.Errorf("a range past the file says what the file has, got %q", got)
	}
}

// An attachment renders in place or refuses in place — it never silently vanishes, and it never
// escapes the workspace jail.
func TestRefsRenderOrRefuseInPlace(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	const sid session.SessionID = "s_refs"
	wd := t.TempDir()
	d, _ := json.Marshal(event.SessionCreatedData{Workdir: wd})
	if err := a.appendFact(ctx, sid, event.TypeSessionCreated,
		event.Actor{Kind: event.ActorSystem, ID: "test"}, d); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wd, "notes.md"), []byte("alpha\nbeta\ngamma"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := command.SubmitPrompt{SessionID: sid,
		Parts: []session.Part{{Kind: session.PartText, Text: "look at these"}},
		Refs: []command.FileRef{
			{Path: "notes.md", Lines: "2-3"},
			{Path: "../escape.txt"},
			{Path: "absent.md"},
		}}
	a.appendRefs(ctx, &cmd)
	if len(cmd.Parts) != 2 {
		t.Fatalf("the refs ride as one appended block, got %d parts", len(cmd.Parts))
	}
	block := cmd.Parts[1].Text
	if !strings.Contains(block, "beta\ngamma") || strings.Contains(block, "alpha") {
		t.Fatalf("the named lines and only them: %q", block)
	}
	if !strings.Contains(block, "outside this workspace") {
		t.Fatalf("the jail refuses in place, in words: %q", block)
	}
	if !strings.Contains(block, "absent.md") || !strings.Contains(block, "not shown") {
		t.Fatalf("a missing file is said, not skipped: %q", block)
	}
	if cmd.Parts[0].Text != "look at these" {
		t.Fatal("the person's words stay the person's words")
	}

	// No refs, no block — and an oversized attachment is cut at the cap.
	plain := command.SubmitPrompt{SessionID: sid, Parts: cmd.Parts[:1]}
	a.appendRefs(ctx, &plain)
	if len(plain.Parts) != 1 {
		t.Fatal("nothing attached appends nothing")
	}
	big := strings.Repeat("x", refCap+100)
	if err := os.WriteFile(filepath.Join(wd, "big.txt"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	bcmd := command.SubmitPrompt{SessionID: sid, Refs: []command.FileRef{{Path: "big.txt"}}}
	a.appendRefs(ctx, &bcmd)
	if got := bcmd.Parts[0].Text; len(got) > refCap+400 || !strings.Contains(got, "not shown") {
		t.Fatalf("the cap holds and says so: %d bytes", len(got))
	}
}
