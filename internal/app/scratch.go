package app

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/core/session"
)

// A turn owns a scratch directory, and the turn is exactly how long it lives.
//
// It answers two things that were being answered by the model writing shell:
//
//   - Where does a command's output go. magi already captured it to a file and deleted the file
//     when the call returned (runCapture), so the model composed `> log 2>&1` to get a copy it
//     could read later and `| tail -100` to bound what came back — and both of those cost it the
//     exit code, because a redirect and a pipe move the status to something that is not the
//     command. Keeping the file for the turn removes the reason to write either one.
//   - Where does a scratch FILE go. With no answer, an agent puts it in the workspace: observed as
//     `cp /app/input.csv /app/test_input.csv` and `cp /app/input.csv /app/input_backup.csv` on a
//     task whose deliverable is that very tree, left behind after the run. TMPDIR points here now,
//     so mktemp, python's tempfile, a compiler's intermediates — everything that ASKS for a temp
//     path gets one outside the deliverable, with no model awareness at all.
//
// Removed whole when the turn ends. That boundary is the design: anything that has to outlive the
// turn is a deliverable and belongs in the workspace, which means no quota, no rotation policy, and
// no rule about when a scratch file is stale — it is stale when the turn is over.
type turnScratch struct {
	root string // /tmp/magi-turn-*
	logs string // root/logs — magi's captured command output
	tmp  string // root/tmp  — the command's TMPDIR
}

// staleScratchAge bounds how long an orphan survives. A turn removes its own directory; one is left
// behind only when the process died without unwinding, so anything older than this had no owner.
const staleScratchAge = 6 * time.Hour

// newTurnScratch creates the directory for one turn. A failure returns nil rather than an error —
// every caller degrades to the previous behavior (capture to a temp file and delete it), because
// losing the scratch must never cost the turn.
func newTurnScratch() *turnScratch {
	root, err := os.MkdirTemp("", "magi-turn-*")
	if err != nil {
		return nil
	}
	s := &turnScratch{root: root, logs: filepath.Join(root, "logs"), tmp: filepath.Join(root, "tmp")}
	if os.MkdirAll(s.logs, 0o700) != nil || os.MkdirAll(s.tmp, 0o700) != nil {
		_ = os.RemoveAll(root)
		return nil
	}
	go sweepStaleScratch(root) // reclaim what a crashed run left; never blocks the turn
	return s
}

// remove drops the whole directory. Called once, by the turn that created it — a child inherits the
// paths and never removes, or the first child to finish would take its siblings' output with it.
func (s *turnScratch) remove() {
	if s == nil {
		return
	}
	_ = os.RemoveAll(s.root)
}

// logsDir / tmpDir are nil-safe readers, so a caller that has no scratch passes "" and gets the
// pre-scratch behavior without branching.
func (s *turnScratch) logsDir() string {
	if s == nil {
		return ""
	}
	return s.logs
}

func (s *turnScratch) tmpDir() string {
	if s == nil {
		return ""
	}
	return s.tmp
}

// sweepStaleScratch removes turn directories older than staleScratchAge, skipping the live one. A
// turn removes its own, so what this finds is what a kill -9 left behind.
func sweepStaleScratch(keep string) {
	dir := os.TempDir()
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-staleScratchAge)
	for _, e := range ents {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "magi-turn-") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		if p == keep {
			continue
		}
		if info, err := e.Info(); err == nil && info.ModTime().Before(cutoff) {
			_ = os.RemoveAll(p)
		}
	}
}

// scratchFor returns the scratch a session runs under: its own if it started a turn, otherwise the
// one it inherited when it was spawned. Nil when there is none.
func (a *App) scratchFor(sid session.SessionID) *turnScratch {
	a.mu.Lock()
	defer a.mu.Unlock()
	if st, ok := a.stateIf(sid); ok {
		return st.scratch
	}
	return nil
}

// setScratch records a session's scratch — the turn that created it, or a child inheriting the
// parent's pointer at spawn.
func (a *App) setScratch(sid session.SessionID, s *turnScratch) {
	a.mu.Lock()
	a.stateLocked(sid).scratch = s
	a.mu.Unlock()
}
