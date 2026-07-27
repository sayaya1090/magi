package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/command"
)

func TestCitedPaths(t *testing.T) {
	got := citedPaths("File /app/pool_sweep_analysis.txt verified: exists (4826 bytes, 96 lines), " +
		"see `docs/notes.md` and http://example.com/x.html and /app/build/ and README")
	want := []string{"/app/pool_sweep_analysis.txt", "docs/notes.md"}
	if len(got) != len(want) {
		t.Fatalf("citedPaths = %q; want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("citedPaths[%d] = %q; want %q", i, got[i], want[i])
		}
	}
	if len(citedPaths(strings.Repeat("a/b.c ", 20))) != reportPathCap {
		t.Errorf("a report naming a whole tree must be capped at %d", reportPathCap)
	}
}

// Live, a worker composed its own analysis file, reported done, and filed
// "EVIDENCE: File /app/pool_sweep_analysis.txt verified: exists (4826 bytes, 96 lines)" — true in
// every word, and evidence of nothing but what it typed. The check gate's provenance audit never
// saw it, because a report is not a check.
func TestReportEvidenceCitingItsOwnWriteIsAnnotated(t *testing.T) {
	app := newShellApp(t, &shellPlatform{})
	ctx := context.Background()
	wd := t.TempDir()
	sid, err := app.CreateSession(ctx, command.CreateSession{Workdir: wd})
	if err != nil {
		t.Fatal(err)
	}
	if _, aerr := app.store.Append(ctx, sid, toolCallEvent("write",
		`{"path":"/app/pool_sweep_analysis.txt","content":"ANALYSIS OF RUN-LENGTH COMPRESSION DEFECT\n"}`)); aerr != nil {
		t.Fatal(aerr)
	}
	// A file a real command produced, in the same report — it must not be tarred with the other.
	if _, aerr := app.store.Append(ctx, sid, toolCallEvent("bash",
		`{"command":"make -C testsuite one DIR=tests/basic > /app/logs/suite.log 2>&1"}`)); aerr != nil {
		t.Fatal(aerr)
	}

	note := app.auditReportEvidence(ctx, sid,
		"File /app/pool_sweep_analysis.txt verified: exists (4826 bytes, 96 lines)")
	for _, want := range []string{"PROVENANCE:", "/app/pool_sweep_analysis.txt", "`write`", "not what the work did"} {
		if !strings.Contains(note, want) {
			t.Errorf("a self-authored citation must be named: %q missing from %q", want, note)
		}
	}
	// A path nothing in this run wrote is silent — the audit reports, it does not speculate.
	if n := app.auditReportEvidence(ctx, sid, "see /app/ocaml/runtime/shared_heap.c"); n != "" {
		t.Errorf("an unauthored path must yield no note, got %q", n)
	}
	// Evidence with no path at all is silent too.
	if n := app.auditReportEvidence(ctx, sid, "the basic testsuite runs cleanly"); n != "" {
		t.Errorf("prose with no citation must yield no note, got %q", n)
	}

	// The note renders next to the evidence it qualifies, never apart from it.
	rep := &subReport{status: "done", evidence: "File /app/pool_sweep_analysis.txt verified", provenance: note}
	out := rep.result("analysis complete")
	ei, pi := strings.Index(out, "EVIDENCE:"), strings.Index(out, "PROVENANCE:")
	if ei < 0 || pi < 0 || pi < ei {
		t.Errorf("PROVENANCE must follow EVIDENCE in the rendered report:\n%s", out)
	}
}
