package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
)

// The bash path already refuses to count a mutation whose every destination came back holding the
// bytes it already held. The write path saw the same evidence and did not use it: noteEdit reports
// a byte-identical write as regressed=false — nothing moved either way — and the only retraction
// there was gated on regressed.
//
// mutated() catches the plainest shape on its own: the same args to the same path is one
// signature, and the second one is not counted. What gets through is an identical write that
// arrives with a NEW signature, and the shared mutation record is what hands it one — the same
// interleaving TestAScratchRedirectDoesNotRearmAnIdenticalWrite is built on. Every write below
// leaves the file exactly as it found it, and each one used to restart the no-progress window.
func TestAnIdenticalWriteIsNotProgress(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "extract.js")
	const body = "const fs = require('fs');\n"
	if err := os.WriteFile(target, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow"})
	g := newRunGuard()

	// One write call. The bytes written are always what the file already holds; only the tool
	// call's identity varies, which is what a fresh signature amounts to.
	write := func(content, callID string) {
		args, err := json.Marshal(map[string]string{"path": target, "content": content, "id": callID})
		if err != nil {
			t.Fatal(err)
		}
		tc := &session.ToolCall{CallID: callID, Name: "write", Args: args}
		res := session.ToolResult{CallID: callID, Content: json.RawMessage(`"wrote 26 bytes"`)}
		g.sinceProgress++ // stands in for check()'s increment on the call itself
		before, _ := readForChange(dir, target)
		if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		a.noteToolOutcome(sid, g, toolOutcome{
			tc: tc, res: &res, workdir: dir, fp: "write:" + callID, novel: true, toolOK: true,
			changePath: target, changeBefore: before,
		})
	}

	for i := 0; i < noProgressNudge+2; i++ {
		write(body, "c"+strconv.Itoa(i)) // the same bytes, a new signature every time
	}
	if g.sinceProgress < noProgressNudge+2 {
		t.Fatalf("a run that only rewrote the same bytes has made no progress, got sinceProgress=%d",
			g.sinceProgress)
	}
	if g.sinceProgress-g.lastStallAt < noProgressNudge {
		t.Errorf("the stall window must reach its threshold across the loop (window=%d, need %d)",
			g.sinceProgress-g.lastStallAt, noProgressNudge)
	}

	// The control that must not regress: a write that really changes the file is real work and
	// restarts the window. Cancelling that would be the false stall this guard exists to avoid.
	write(body+"// a real change\n", "real")
	if g.sinceProgress != 0 {
		t.Errorf("a genuinely new version is progress and restarts the window, got %d", g.sinceProgress)
	}
}

// Unknown is not unchanged. A write magi could not read back leaves the two sides unequal, and
// nothing is retracted — the same rule the bash path states as compared > 0.
func TestAWriteMagiCouldNotReadBackKeepsItsProgress(t *testing.T) {
	dir := t.TempDir()
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow"})
	g := newRunGuard()
	missing := filepath.Join(dir, "never-written.js")

	args, err := json.Marshal(map[string]string{"path": missing, "content": "x"})
	if err != nil {
		t.Fatal(err)
	}
	tc := &session.ToolCall{CallID: "c", Name: "write", Args: args}
	res := session.ToolResult{CallID: "c", Content: json.RawMessage(`"wrote 1 bytes"`)}
	g.sinceProgress += 5
	a.noteToolOutcome(sid, g, toolOutcome{
		tc: tc, res: &res, workdir: dir, fp: "write:x", novel: true, toolOK: true,
		changePath: missing, changeBefore: "", // absent before; still absent after (the write is mocked away)
	})
	// Empty on both sides is the shape magi cannot read: `write{""}` on a missing path CREATES a
	// file and on an empty one changes nothing. It must not be scored either way.
	if g.sinceProgress != 0 {
		t.Errorf("an unreadable pair must leave the counter where mutated() put it, got %d", g.sinceProgress)
	}
}

// Two writes of the same bytes to DIFFERENT files are two real creations, not one no-op. The
// comparison is per call and per path, and must not fold across paths.
func TestIdenticalBytesToADifferentFileIsStillProgress(t *testing.T) {
	dir := t.TempDir()
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow"})
	g := newRunGuard()
	const body = "shared boilerplate\n"

	for i, name := range []string{"a.js", "b.js"} {
		p := filepath.Join(dir, name)
		args, err := json.Marshal(map[string]string{"path": p, "content": body})
		if err != nil {
			t.Fatal(err)
		}
		before, _ := readForChange(dir, p)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		g.sinceProgress += 3
		tc := &session.ToolCall{CallID: "c" + strconv.Itoa(i), Name: "write", Args: args}
		res := session.ToolResult{CallID: "c" + strconv.Itoa(i), Content: json.RawMessage(`"wrote 19 bytes"`)}
		a.noteToolOutcome(sid, g, toolOutcome{
			tc: tc, res: &res, workdir: dir, fp: "write:" + name, novel: true, toolOK: true,
			changePath: p, changeBefore: before,
		})
		if g.sinceProgress != 0 {
			t.Errorf("%s: creating a file is progress even when its bytes match another file, got %d",
				name, g.sinceProgress)
		}
	}
}
