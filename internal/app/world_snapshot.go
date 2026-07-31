package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// magi's record answers "what happened". It cannot answer "what is there now", and at the moment a
// turn ends those are different questions. The record is a replay of calls magi granted; the
// workspace is the thing that will actually be judged, and between the last call and the finish it
// can hold a file no call in the record wrote — a build artifact, a shell redirect magi could not
// parse, a script's own output — or be missing one the record says was written.
//
// So the finish declaration carries a fresh read of the workspace, taken at the instant it is made.
// It is the closest magi has to what a screen-driven agent shows when it asks "are you sure": a
// statement about the world rather than about the transcript, able to contradict both the agent's
// claim and magi's own record.

const (
	// snapshotFileCap bounds how many paths the snapshot names. A finish that touched more than
	// this has not produced a result anyone can read at a glance, and the point is to be read.
	snapshotFileCap = 40
	// snapshotWalkCap bounds the walk itself, so a workspace with a giant tree costs a bounded
	// read rather than a directory crawl at the one moment the turn is trying to end.
	snapshotWalkCap = 20000
	// snapshotPerDirCap is how many files one directory may name before it is collapsed to a count.
	// Past this the individual names stop carrying information — a build wrote its objects — and
	// start crowding out the file the task was actually about.
	snapshotPerDirCap = 4
)

// snapshotSkipDirs are trees whose churn says nothing about the deliverable: version-control
// internals, dependency and build output, caches. Skipping them is what keeps the snapshot short
// enough to be read rather than scrolled past.
var snapshotSkipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "dist": true, "build": true,
	"target": true, "__pycache__": true, ".magi": true, ".cache": true, ".venv": true,
}

// worldSnapshot reads the workspace as it stands RIGHT NOW and reports the files modified at or
// after since — the ones this turn is responsible for — with their current size and age. Reading
// only, and best-effort: an unreadable tree yields "" rather than an error, because a snapshot that
// cannot be taken must not read as a workspace that is empty.
func worldSnapshot(workdir string, since time.Time) string {
	if strings.TrimSpace(workdir) == "" {
		return ""
	}
	type entry struct {
		path string
		size int64
		mod  time.Time
	}
	var hits []entry
	var skipped []string // trees the walk did not enter, so the snapshot can say so
	skippedN := 0        // …and how many there were, since the NAMES are capped and that cut counts too
	seen := 0
	_ = filepath.WalkDir(workdir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // an unreadable branch is skipped, never fatal
		}
		if seen++; seen > snapshotWalkCap {
			return filepath.SkipAll
		}
		if d.IsDir() {
			if p != workdir && (snapshotSkipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
				skippedN++
				if rel, rerr := filepath.Rel(workdir, p); rerr == nil && len(skipped) < snapshotSkipNameCap {
					skipped = append(skipped, rel+"/")
				}
				return filepath.SkipDir
			}
			return nil
		}
		fi, err := d.Info()
		if err != nil || fi.ModTime().Before(since) {
			return nil
		}
		rel, err := filepath.Rel(workdir, p)
		if err != nil {
			rel = p
		}
		hits = append(hits, entry{rel, fi.Size(), fi.ModTime()})
		return nil
	})
	if len(hits) == 0 {
		// The absence claim is the one a reader acts on hardest, and it was the one that could be
		// false: the walk does not enter vendor/, build/, dist/, target/ or any dotdir, so a
		// deliverable built into one of them produced "no file has been modified" under a heading
		// that says THE WORKSPACE RIGHT NOW. Measured against a workspace holding nothing but
		// vendor/sqlite/sqlite3.c — which is the shape of a task whose source IS pre-vendored.
		// Say what was not looked at, and the sentence is true again.
		return "── THE WORKSPACE RIGHT NOW (read just now, not from the record) ──\n" +
			"no file in the workspace has been modified since this task started" + skipNote(skipped, skippedN) + "."
	}
	// Newest last: the tail is what just happened, which is what a reader looks for first.
	sort.Slice(hits, func(i, j int) bool { return hits[i].mod.Before(hits[j].mod) })

	// A build writes hundreds of object files, and naming them individually would push the one file
	// the task is about off the end of the list. So a directory with more than a handful of modified
	// files is collapsed to a count and its newest member — the fact that it churned is the signal;
	// the names are not. This is a shape rule, not a list of directories to ignore: nothing is judged
	// by its name, only by how many of its files this turn touched.
	byDir := map[string][]entry{}
	var order []string
	for _, h := range hits {
		d := filepath.Dir(h.path)
		if _, ok := byDir[d]; !ok {
			order = append(order, d)
		}
		byDir[d] = append(byDir[d], h)
	}
	var lines []string
	for _, d := range order {
		es := byDir[d]
		if len(es) <= snapshotPerDirCap {
			for _, h := range es {
				lines = append(lines, fmt.Sprintf("%s — %d bytes, modified %s ago", h.path, h.size,
					fmtElapsed(time.Since(h.mod))))
			}
			continue
		}
		newest := es[len(es)-1]
		where := d + "/"
		if d == "." {
			where = "the workspace root"
		}
		lines = append(lines, fmt.Sprintf("%s — %d files modified (newest: %s, %d bytes, %s ago)",
			where, len(es), filepath.Base(newest.path), newest.size, fmtElapsed(time.Since(newest.mod))))
	}
	trimmed := false
	if len(lines) > snapshotFileCap {
		lines = lines[len(lines)-snapshotFileCap:]
		trimmed = true
	}
	var b strings.Builder
	b.WriteString("── THE WORKSPACE RIGHT NOW (read just now, not from the record) ──")
	if trimmed {
		b.WriteString(fmt.Sprintf("\n(the %d most recent)", snapshotFileCap))
	}
	for _, l := range lines {
		b.WriteString("\n" + l)
	}
	if n := skipNote(skipped, skippedN); n != "" {
		b.WriteString("\n(this listing is complete" + n + ")")
	}
	return b.String()
}

// snapshotSkipNameCap bounds how many skipped trees are named. Past a handful the names stop
// telling a reader anything and start being the noise the skip rule exists to remove.
const snapshotSkipNameCap = 6

// skipNote renders what the walk did not enter. Every other cut magi makes is marked — a tool
// result, the evidence block, the session list, a compaction — and this one was not, which is what
// let a heading reading "THE WORKSPACE RIGHT NOW" sit above a claim about part of it.
func skipNote(skipped []string, total int) string {
	if total == 0 {
		return ""
	}
	sort.Strings(skipped)
	more := ""
	if n := total - len(skipped); n > 0 {
		// The names are capped, and a capped list that does not say so is the very thing this
		// note exists to stop — one unmarked omission traded for another.
		more = fmt.Sprintf(" and %d more", n)
	}
	return " outside " + strings.Join(skipped, ", ") + more + ", which this read does not enter"
}

// liveJobsNow reports the background commands as they stand right now: still running, or how they
// ended. A file listing cannot say this — a server the agent started, a build still compiling, or a
// job that exited nonzero after the last tool call are all part of the world the finish is about to
// be judged in, and none of them leaves a mark in the file tree.
func (a *App) liveJobsNow(mine map[string]bool) string {
	jobs := a.BackgroundJobs()
	if len(jobs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("── BACKGROUND COMMANDS RIGHT NOW ──")
	n := 0
	for _, j := range jobs {
		if !mine[j.ID] {
			continue // another session's process registry entry — not this run's world
		}
		n++
		switch {
		case j.Running:
			b.WriteString(fmt.Sprintf("\n%s still RUNNING after %s — %s", j.ID,
				fmtElapsed(time.Since(j.Started)), clipLine(j.Command, 70)))
		case j.Killed:
			b.WriteString(fmt.Sprintf("\n%s was killed — %s", j.ID, clipLine(j.Command, 70)))
		default:
			b.WriteString(fmt.Sprintf("\n%s exited %d — %s", j.ID, j.Exit, clipLine(j.Command, 70)))
		}
	}
	if n == 0 {
		return ""
	}
	return b.String()
}

// jobsStartedBy reads the session's own record for the ids bash handed back when it started a
// background command. The process registry is global — processes are — so without this a second
// session would be shown a job it never started and told it was part of its world.
func (a *App) jobsStartedBy(ctx context.Context, sid session.SessionID) map[string]bool {
	return jobIDsIn(a.readEventsBestEffort(ctx, sid))
}

// jobIDsIn is jobsStartedBy over events already in hand — the per-step path has just read them, and
// reading the log again for every step is how a cheap block becomes the loop's dominant cost.
func jobIDsIn(evs []event.Event) map[string]bool {
	out := map[string]bool{}
	for _, e := range evs {
		if e.Type != event.TypePartAppended {
			continue
		}
		var d event.PartAppendedData
		if json.Unmarshal(e.Data, &d) != nil || d.Part.Kind != session.PartToolResult || d.Part.ToolResult == nil {
			continue
		}
		txt := decodeResultText(string(d.Part.ToolResult.Content))
		for _, id := range bgIDsIn(txt) {
			out[id] = true
		}
	}
	return out
}

// bgIDsIn pulls the "started background command bg_N" ids out of a tool result.
func bgIDsIn(s string) []string {
	var out []string
	const marker = "started background command "
	for i := strings.Index(s, marker); i >= 0; i = strings.Index(s, marker) {
		s = s[i+len(marker):]
		j := 0
		for j < len(s) && (s[j] == '_' || s[j] == 'b' || s[j] == 'g' || (s[j] >= '0' && s[j] <= '9')) {
			j++
		}
		if j > 0 {
			out = append(out, s[:j])
		}
		s = s[j:]
	}
	return out
}

// missingFromWorld names the paths magi RECORDED as written that are not on disk now. It is the one
// contradiction a record can never surface on its own: every write in the log succeeded, and the
// file is gone anyway — cleaned up by a later command, written to a temp dir, or never where the
// agent thought it was. Empty when the record and the disk agree.
func missingFromWorld(workdir string, recorded []string) []string {
	var gone []string
	for _, p := range recorded {
		full := p
		if !filepath.IsAbs(full) {
			full = filepath.Join(workdir, p)
		}
		if _, err := os.Stat(full); err != nil {
			gone = append(gone, p)
		}
	}
	return gone
}

// runState is the per-step state block: what magi's record says the run has done, plus any
// background command still alive. Both are read fresh at the moment it is rendered, from the store
// and the process registry — no part of it is the agent's own narration.
//
// It is deliberately the RECORD and not the filesystem. The record is magi's state store the way a
// terminal is a screen-driven agent's: everything that happened is in it, and the context is a view
// of it. Walking the disk answers a different question and belongs where that question is asked —
// at the finish, where "the record says written, the disk says no" is worth the walk.
func (a *App) runState(evs []event.Event) string {
	var parts []string
	if rec := observeEvents(evs).render(); rec != "" {
		parts = append(parts, rec)
	}
	if jobs := a.liveJobsNow(jobIDsIn(evs)); jobs != "" {
		parts = append(parts, jobs)
	}
	return strings.Join(parts, "\n\n")
}
