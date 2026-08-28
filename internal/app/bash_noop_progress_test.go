package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
)

// mutated() already refuses to count an idempotent rewrite as progress — but the signature it
// compares on the bash path is the command TEXT, so a command differing by one character is a new
// signature no matter what it wrote.
//
// Observed live (extract-elf, 2026-07-29): eleven runs of
// `node extract.js /app/a.out > out.json && python3 -c "…"` over ten minutes, each varying the
// one-liner, each regenerating out.json byte for byte. Every one reset the no-progress counter, so
// the stall nudge could never come due — while the same results carried "this write left the file
// byte-for-byte as it already was" eleven times over.
func TestAnIdenticalRegenerationIsNotProgress(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.json")
	const body = `{"0":1179403647,"4":65794}`
	if err := os.WriteFile(out, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow"})
	g := newRunGuard(nil)

	run := func(cmd string, changes []bashChange) {
		tc := &session.ToolCall{CallID: "c", Name: "bash",
			Args: json.RawMessage(`{"command":` + strconv.Quote(cmd) + `}`)}
		res := session.ToolResult{CallID: "c", Content: json.RawMessage(`"exit 0"`)}
		g.sinceProgress++ // stands in for check()'s increment on the call itself
		a.noteToolOutcome(sid, g, toolOutcome{
			tc: tc, res: &res, workdir: dir, fp: cmd, novel: true, toolOK: true,
			bashChanges: changes,
		})
	}
	same := []bashChange{{path: out, before: body, readable: true}}

	// Enough regenerations to reach the stall threshold, each a DIFFERENT command text, each
	// writing the same bytes.
	for i := 0; i < noProgressNudge+2; i++ {
		run("node extract.js > out.json && python3 -c \"print("+strconv.Itoa(i)+")\"", same)
	}
	if g.sinceProgress < noProgressNudge+2 {
		t.Fatalf("a run that only rewrote the same bytes has made no progress, got sinceProgress=%d",
			g.sinceProgress)
	}
	if g.sinceProgress-g.lastStallAt < noProgressNudge {
		t.Errorf("the stall window must reach its threshold across the loop (window=%d, need %d)",
			g.sinceProgress-g.lastStallAt, noProgressNudge)
	}

	// The control that must not regress: a destination whose bytes really changed is real work and
	// restarts the window, even reached by yet another command text.
	if err := os.WriteFile(out, []byte(body+"x"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(`node extract.js > out.json && echo done`, []bashChange{{path: out, before: body, readable: true}})
	if g.sinceProgress != 0 {
		t.Errorf("a genuinely new version is progress and restarts the window, got %d", g.sinceProgress)
	}

	// One changed destination among several unchanged ones is still real work: cancelling it
	// because a sibling was rewritten identically would be the false stall this guard avoids.
	other := filepath.Join(dir, "other.json")
	if err := os.WriteFile(other, []byte("moved"), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		g.sinceProgress++
	}
	run(`node extract.js > out.json && node other.js > other.json`, []bashChange{
		{path: out, before: body + "x", readable: true}, // unchanged
		{path: other, before: "was", readable: true},    // changed
	})
	if g.sinceProgress != 0 {
		t.Errorf("one destination that really moved is progress, got %d", g.sinceProgress)
	}

	// A command magi could not read either side of stays out of it: unknown is not unchanged.
	for i := 0; i < 3; i++ {
		g.sinceProgress++
	}
	before := g.sinceProgress
	run(`tar -xzf pkg.tgz -C build`, []bashChange{{path: filepath.Join(dir, "build"), readable: false}})
	if g.sinceProgress >= before {
		t.Errorf("an unreadable destination cannot be called unchanged; the reset stands, got %d", g.sinceProgress)
	}
}
