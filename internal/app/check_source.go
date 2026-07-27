package app

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/session"
)

// A check's `source` is either a file the step must PRODUCE or a file that already exists and the
// step must change. The gate cannot tell those apart, and it must not: an absent source is reported
// as a FAILURE precisely because "the step was supposed to record it here" is a fact about the
// deliverable.
//
// That leaves one shape it reads wrong. A check that names an existing file by the WRONG PATH looks
// exactly like a record that was never produced — it fails, it keeps failing however good the work
// is, and the failure drives re-planning of a step that has nothing wrong with it. Observed live:
//
//	step 5  source: caml/major_gc.c   assert: matches /caml_fl_sweep.*run.*compress|…/
//
// `caml/major_gc.c` does not exist. `ocaml/runtime/major_gc.c` does. The step's work had gone into
// `runtime/shared_heap.c` and was rewritten seven times across three worker attempts and 45 minutes,
// and the check could not have passed for any of them.
//
// magi can settle this without asking anyone: it has the filesystem. A path that does not resolve,
// whose BASENAME resolves at exactly one place in the workspace, is a misspelling of that place —
// not a guess, a lookup. Two matches is a guess, so it is only reported; no match is the legitimate
// case (a record the step still has to write) and is left completely alone.

// checkSourceRepairEnabled gates the pass (default ON; MAGI_CHECK_SOURCE_REPAIR=0 disables).
func checkSourceRepairEnabled() bool { return !envOff("MAGI_CHECK_SOURCE_REPAIR") }

// sourceScanCap bounds the workspace walk. A repository large enough to exceed it is one where a
// basename lookup would be ambiguous anyway, and the pass gives up rather than spend the turn.
const sourceScanCap = 200000

// repairCheckSources rewrites a check whose `source` names an existing file by the wrong path, and
// reports the ones it cannot settle. Returns the checks, changed or not — this never drops one: a
// check magi could not place is still the council's contract, and the run is better off failing it
// honestly than losing it silently.
func (a *App) repairCheckSources(ctx context.Context, s session.Session, checks []council.DeliverableCheck) []council.DeliverableCheck {
	if !checkSourceRepairEnabled() || strings.TrimSpace(s.Workdir) == "" {
		return checks
	}
	var missing []string
	for _, c := range checks {
		if src := strings.TrimSpace(c.Source); src != "" && readsAFile(c.Assert) && !resolvesUnder(s.Workdir, src) {
			missing = append(missing, src)
		}
	}
	if len(missing) == 0 {
		return checks
	}
	found := findByBasename(s.Workdir, missing)
	out := make([]council.DeliverableCheck, len(checks))
	copy(out, checks)
	for i, c := range out {
		src := strings.TrimSpace(c.Source)
		if src == "" || !readsAFile(c.Assert) || resolvesUnder(s.Workdir, src) {
			continue
		}
		switch hits := found[filepath.Base(filepath.FromSlash(src))]; len(hits) {
		case 1:
			out[i].Source = hits[0]
			a.emitToolProgress(s.ID, plannerActor, "", "check-source", fmt.Sprintf(
				"check-source: %q does not exist, and %q is the only file by that name in the workspace — "+
					"the check for %q now reads it. A path that cannot resolve fails forever, and that failure "+
					"reads as a broken deliverable rather than a broken check.",
				src, hits[0], clipLine(strings.TrimSpace(c.Deliverable), 60)))
		case 0:
			// Nothing by that name anywhere: the ordinary case of a record the step must still
			// write. Left exactly as authored — the gate's "absent source" failure is the right
			// answer there and this pass must not take it away.
		default:
			a.emitToolProgress(s.ID, plannerActor, "", "check-source", fmt.Sprintf(
				"check-source: %q does not exist and %d files share that name (%s) — leaving the check for %q "+
					"as authored, since choosing between them would be a guess. If it fails, the path is the "+
					"first thing to doubt.",
				src, len(hits), strings.Join(clipEach(hits, 3), ", "), clipLine(strings.TrimSpace(c.Deliverable), 60)))
		}
	}
	return out
}

// readsAFile reports whether an assertion's subject is a file at all. The liveness probes read the
// world (a port, a process), so they have no path to repair.
func readsAFile(assert string) bool {
	switch verb, _, _ := strings.Cut(strings.TrimSpace(assert), " "); verb {
	case "nonempty", "matches", "absent", "equals":
		return true
	}
	return false
}

// resolvesUnder reports whether src names something that exists — absolute as written, or relative
// to the workspace, which is how the check runner reads it.
func resolvesUnder(workdir, src string) bool {
	p := filepath.FromSlash(src)
	if !filepath.IsAbs(p) {
		p = filepath.Join(workdir, p)
	}
	_, err := os.Stat(p)
	return err == nil
}

// findByBasename walks the workspace once and collects, for each wanted basename, the
// workspace-relative paths that carry it. One walk for all of them: a per-check walk on a large
// repository is the kind of cost that turns a safety pass into a reason to disable it.
func findByBasename(workdir string, srcs []string) map[string][]string {
	want := map[string]bool{}
	for _, s := range srcs {
		want[filepath.Base(filepath.FromSlash(strings.TrimSpace(s)))] = true
	}
	out := map[string][]string{}
	seen := 0
	_ = filepath.WalkDir(workdir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // an unreadable corner is not a reason to abandon the rest
		}
		if seen++; seen > sourceScanCap {
			return fs.SkipAll
		}
		if d.IsDir() {
			// Skip what a deliverable never lives in, so the walk stays about the workspace: version
			// control internals and dependency trees are large, and a match inside one would name a
			// path no check should assert on anyway.
			if n := d.Name(); p != workdir && (strings.HasPrefix(n, ".") || n == "node_modules" || n == "vendor") {
				return fs.SkipDir
			}
			return nil
		}
		if !want[d.Name()] {
			return nil
		}
		if rel, rerr := filepath.Rel(workdir, p); rerr == nil {
			out[d.Name()] = append(out[d.Name()], filepath.ToSlash(rel))
		}
		return nil
	})
	return out
}

// clipEach returns at most n entries, with a marker when more were dropped.
func clipEach(xs []string, n int) []string {
	if len(xs) <= n {
		return xs
	}
	return append(append([]string{}, xs[:n]...), fmt.Sprintf("…and %d more", len(xs)-n))
}
