package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
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

// The budget counts everything it renders — headers included — and the ref that crosses the line
// is clipped at the line. Hunted: hundreds of tiny refs, each header free, and the 64KB the cap
// exists to hold was gone.
func TestRefsBudgetCountsHeadersAndClipsAtTheLine(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	const sid session.SessionID = "s_budget"
	wd := t.TempDir()
	d, _ := json.Marshal(event.SessionCreatedData{Workdir: wd})
	if err := a.appendFact(ctx, sid, event.TypeSessionCreated,
		event.Actor{Kind: event.ActorSystem, ID: "test"}, d); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wd, "tiny.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	refs := make([]command.FileRef, 0, 5000)
	for i := 0; i < 5000; i++ {
		refs = append(refs, command.FileRef{Path: "tiny.txt"})
	}
	cmd := command.SubmitPrompt{SessionID: sid, Refs: refs}
	a.appendRefs(ctx, &cmd)
	block := cmd.Parts[0].Text
	if len(block) > refsCap+refCap+512 {
		t.Fatalf("the budget did not hold: %d bytes rendered", len(block))
	}
	if !strings.Contains(block, "more attachment(s) not shown") {
		t.Fatal("past the line, the rest fold to one closing line")
	}
}

// The builtin path is trimmed at the source, so a padded path cannot split one file across two
// guard slots (the FileTool branch already trimmed; the asymmetry was the hunt's finding).
func TestTouchesFileInTrimsTheBuiltinPath(t *testing.T) {
	touch, ok := touchesFileIn(nil, "write", json.RawMessage(`{"path":"  x.txt  "}`))
	if !ok || touch.path != "x.txt" || touch.guard != "x.txt" {
		t.Fatalf("one file, one slot, whatever the padding: %+v", touch)
	}
}

// The observer hears the person's words and never the rendered attachment block — workspace
// bytes reach a plugin through a file grant, not through a user-message side channel.
type earTest struct{ heard []string }

func (e *earTest) UserMessage(_ string, text string)    { e.heard = append(e.heard, text) }
func (e *earTest) TurnFinished(string, TurnObservation) {}

func TestObserverHearsWordsNotAttachments(t *testing.T) {
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ear := &earTest{}
	a := closeAfter(t, New(store, completingLLM{}, builtin.Default(), bus.New(), nil,
		Config{Permission: "allow", Observer: ear}))
	ctx := context.Background()
	wd := t.TempDir()
	if err := os.WriteFile(filepath.Join(wd, "s.txt"), []byte("secret-ish bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	sid, err := a.CreateSession(ctx, command.CreateSession{Workdir: wd,
		Actor: event.Actor{Kind: event.ActorUser, ID: "cli"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Submit(ctx, command.SubmitPrompt{SessionID: sid,
		Actor: event.Actor{Kind: event.ActorUser},
		Parts: []session.Part{{Kind: session.PartText, Text: "read the attached"}},
		Refs:  []command.FileRef{{Path: "s.txt"}}}); err != nil {
		t.Fatal(err)
	}
	if len(ear.heard) != 1 || !strings.Contains(ear.heard[0], "read the attached") {
		t.Fatalf("the person's words reach the ear: %q", ear.heard)
	}
	if strings.Contains(ear.heard[0], "secret-ish") || strings.Contains(ear.heard[0], "ATTACHED BY THE USER") {
		t.Fatalf("workspace bytes must not ride the observation: %q", ear.heard[0])
	}
	// And the AGENT still sees the excerpt: it is in the persisted prompt.
	evs, _ := a.store.Read(ctx, sid, 0)
	saw := false
	for _, e := range evs {
		if strings.Contains(string(e.Data), "secret-ish") {
			saw = true
		}
	}
	if !saw {
		t.Fatal("excluding the observer must not exclude the agent")
	}
}
