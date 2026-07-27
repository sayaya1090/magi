package app

import (
	"strings"
	"testing"
)

// A step's finding used to reach the next worker under one label: "Already produced by earlier
// steps — build on these, don't rebuild". A step that FAILED was therefore announced as a finished
// input the next worker should build on and must not redo — the opposite of what happened. The two
// must reach the worker under labels that disagree about whether the work exists.
func TestDelegateBriefSeparatesFailuresFromOutputs(t *testing.T) {
	steps := []planStep{{Title: "storage layer"}, {Title: "HTTP API"}}
	brief := delegateBrief("build a kv service", steps, 1,
		[]string{"### schema (delegated to coder)\nwrote store/schema.go with Get/Put"},
		[]string{"### storage layer (delegate FAILED — re-plan or do it yourself)\n(the worker could not open the data dir; this sub-task is unfinished)"})

	iProduced := strings.Index(brief, "wrote store/schema.go")
	iFailed := strings.Index(brief, "could not open the data dir")
	if iProduced < 0 || iFailed < 0 {
		t.Fatalf("both the output and the failure must reach the worker:\n%s", brief)
	}
	// The failure must NOT sit under the build-on-these label — that is the whole defect.
	buildOn := strings.Index(brief, "Already produced")
	notFinished := strings.Index(brief, "did NOT finish")
	if notFinished < 0 {
		t.Fatalf("no label saying the failed step's output does not exist:\n%s", brief)
	}
	if !(buildOn < iProduced && iProduced < notFinished && notFinished < iFailed) {
		t.Errorf("the failure is not under its own label (produced@%d failed@%d, labels %d/%d):\n%s",
			iProduced, iFailed, buildOn, notFinished, brief)
	}
	// A worker must not be told to build on something that isn't there.
	tail := brief[notFinished:]
	for _, want := range []string{"do NOT exist", "blocked"} {
		if !strings.Contains(tail, want) {
			t.Errorf("the failure block must say the output is absent and how to react (%q):\n%s", want, tail)
		}
	}
}

// With nothing failed, the brief must look exactly as it did before the split — no empty section,
// no stray heading for a category with no entries.
func TestDelegateBriefOmitsEmptySections(t *testing.T) {
	brief := delegateBrief("goal", []planStep{{Title: "a"}, {Title: "b"}}, 1, []string{"produced a thing"}, nil)
	if strings.Contains(brief, "did NOT finish") {
		t.Errorf("a failure heading appeared with no failures:\n%s", brief)
	}
	brief = delegateBrief("goal", []planStep{{Title: "a"}, {Title: "b"}}, 1, nil, []string{"### a (delegate FAILED)"})
	if strings.Contains(brief, "Already produced") {
		t.Errorf("a produced heading appeared with no outputs:\n%s", brief)
	}
	if brief = delegateBrief("", nil, 0, nil, nil); brief != "" {
		t.Errorf("nothing to brief must render nothing, got:\n%s", brief)
	}
}

// These lists are append-ordered: the newest entry — the step that ran immediately before this
// worker — is LAST. clipLine keeps the head, so it dropped exactly that entry first. The most
// recent finding is the one the next worker most needs, so the clip must keep the tail.
func TestDelegateBriefClipKeepsTheMostRecentFinding(t *testing.T) {
	var produced []string
	for i := 0; i < 40; i++ {
		produced = append(produced, "### stale step — "+strings.Repeat("x", 60))
	}
	produced = append(produced, "### latest step — wrote cmd/server/main.go")
	brief := delegateBrief("goal", []planStep{{Title: "a"}, {Title: "b"}}, 1, produced, nil)
	if !strings.Contains(brief, "wrote cmd/server/main.go") {
		t.Errorf("the newest finding was clipped away:\n%s", brief)
	}
	if !strings.Contains(brief, "earlier entries omitted") {
		t.Errorf("a truncated record must say so, or a mid-list start reads as the whole record:\n%s", brief)
	}
}

// clipTail is rune-safe (never splits a multi-byte character) and is a no-op under the bound.
func TestClipTail(t *testing.T) {
	if got := clipTail("short", 100); got != "short" {
		t.Errorf("under the bound must pass through, got %q", got)
	}
	s := strings.Repeat("한", 100) // 3 bytes each
	got := clipTail(s, 30)
	body := strings.TrimPrefix(got, "…(earlier entries omitted)\n")
	if body == got {
		t.Fatalf("a truncated value must be marked, got %q", got)
	}
	if strings.ContainsRune(body, '�') {
		t.Errorf("clip split a multi-byte rune: %q", body)
	}
	if !strings.HasSuffix(body, "한") {
		t.Errorf("the TAIL must survive, got %q", body)
	}
	if len(body) > 30 {
		t.Errorf("clip exceeded its bound: %d bytes", len(body))
	}
}

// The curator REPLACES the brief, so the produced/failed distinction has to survive it. A packet
// that carries a missing-output note must render it under its own heading, never folded into the
// "build on this" progress section.
func TestCurateBriefKeepsMissingOutOfProgress(t *testing.T) {
	out := renderCurateBrief(curatePacket{
		Goal:     "build a kv service",
		Progress: "store/schema.go defines Get/Put",
		Missing:  "the storage layer was never written — store/disk.go does not exist",
	})
	iProg := strings.Index(out, "store/schema.go")
	iMiss := strings.Index(out, "store/disk.go does not exist")
	if iProg < 0 || iMiss < 0 {
		t.Fatalf("both sections must render:\n%s", out)
	}
	if !strings.Contains(out, "NOT done") {
		t.Errorf("the missing outputs have no heading of their own:\n%s", out)
	}
	// The absent output must not sit inside the build-on-this section.
	prog := out[iProg:iMiss]
	if strings.Contains(prog, "does not exist") {
		t.Errorf("a missing output was rendered inside the progress section:\n%s", out)
	}
	// A packet whose ONLY content is a missing note is still usable — dropping it falls back to the
	// mechanical brief and loses the note entirely.
	if !(curatePacket{Missing: "nothing landed"}).hasContent() {
		t.Error("a missing-only packet must count as content")
	}
	if strings.Contains(renderCurateBrief(curatePacket{Goal: "g"}), "NOT done") {
		t.Error("no missing note must render no heading")
	}
}
