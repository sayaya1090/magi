package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/sayaya1090/magi/internal/core/session"
)

// A report's EVIDENCE section is where a worker proves a "done" claim, and the provenance audit
// never looked at it. It guards the CHECK gate — the assertion magi itself runs — so a worker that
// wrote a file and then cited that same file as proof walked straight past it.
//
// Observed live (22:16→22:20): a worker composed /app/pool_sweep_analysis.txt with its own `write`,
// reported done, ran `ls -la` and `head -20` on it, and filed
//
//	EVIDENCE: File /app/pool_sweep_analysis.txt verified: exists (4826 bytes, 96 lines), contains…
//
// Every word of that is true and none of it is evidence: the bytes it "verified" are the bytes it
// typed. The step's deliverable was a fix to the sweep code.
//
// The same question the check gate asks — who wrote this path — answers it, so this asks it there
// too. It ANNOTATES rather than rejects: a report is prose read by a planner and a council, not an
// assertion magi evaluates, and a worker may legitimately cite a file it authored as long as
// whoever reads the report knows that is what it is.

// reportPathCap bounds how many cited paths one report is audited for. A report naming more paths
// than this is not citing evidence, and the audit's cost is a full event scan per path.
const reportPathCap = 6

// auditReportEvidence returns the provenance note for a report's evidence, or "" when nothing it
// cites was composed by the reporting session's own subtree.
func (a *App) auditReportEvidence(ctx context.Context, sid session.SessionID, evidence string) string {
	if !provenanceEnabled() {
		return ""
	}
	var notes []string
	seen := map[string]bool{}
	for _, p := range citedPaths(evidence) {
		if seen[p] {
			continue
		}
		seen[p] = true
		authors := a.pathAuthors(ctx, sid, p)
		if len(authors) == 0 {
			continue
		}
		notes = append(notes, fmt.Sprintf("%s was written by this worker's own `%s` call", p, authors[0].tool))
	}
	if len(notes) == 0 {
		return ""
	}
	return "PROVENANCE: " + strings.Join(notes, "; ") + ". Citing a file you composed proves what was " +
		"typed, not what the work did — whoever reads this report should weigh it as a claim, not as a result."
}

// citedPaths pulls the file paths a report's prose names. Deliberately narrow: a token has to look
// like a path (a separator, and a base with an extension, or an absolute root) before the audit
// spends a full event scan on it. Over-collecting is harmless — an unauthored path yields no note —
// but every candidate costs a scan, so the cap bounds a report that names a whole tree.
func citedPaths(text string) []string {
	var out []string
	for _, f := range strings.Fields(text) {
		t := strings.Trim(f, "`'\"(),;:!?[]{}<>*_")
		t = strings.TrimSuffix(t, ".")
		if !strings.Contains(t, "/") || strings.Contains(t, "://") {
			continue
		}
		base := t[strings.LastIndex(t, "/")+1:]
		if base == "" || !strings.Contains(base, ".") {
			continue // a directory, or a bare prefix — nothing a file audit can answer about
		}
		if out = append(out, t); len(out) >= reportPathCap {
			break
		}
	}
	return out
}
