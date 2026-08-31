package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// The one gate that runs when nobody is watching.
//
// # The hole this fills
//
// A destructive command already forces a confirmation on top of whatever the permission mode said
// (policy.go's bashDestructive). That works while a person is there. Headless, permission.go
// resolves a forced prompt by policy instead — "no human to ask... resolve by policy" — and under
// `allow`, which is the DAEMON'S DEFAULT, resolving by policy means running it. So the guardrail
// that exists for exactly these commands is the one that does nothing in exactly the situation a
// daemon exists for: work that runs while nobody is watching.
//
// # Why only some of them
//
// Inside a git workspace almost nothing is irreversible. `rm -rf build/`, `git reset --hard`, `git
// clean -fd` all undo from the object store, and treating them as grave costs real work — they are
// ordinary in a build task, and a gate that fires on them would fire constantly and be turned off.
//
// What has no undo is what leaves the tree: a force-push rewriting a remote somebody else pulls,
// a raw device write, a filesystem being made, a recursive delete of a path the workspace does not
// contain. That set is small, and it is rare enough in a normal run that asking costs almost
// nothing while letting one through can cost everything.
//
// So the classification is not by verb, it is by REACH. `rm -rf ./build` and `rm -rf /etc` are the
// same verb and not the same act.
//
// # Why the council rather than a rule
//
// Because a rule is what already ran: deny globs and the destructive scan are deterministic and
// they still let this through, because the question left is not "does this match a pattern" but
// "does this task need this". Only something that can read the task can answer that.
//
// It is NOT a security control and must not be described as one — SECURITY.md says the model's
// judgement never is. A prompt-injected agent can argue for its command as easily as it can issue
// it. This sits ABOVE the deny floor, never in place of it: everything deny refuses is still
// refused before this is reached, and this only ever adds a refusal.

// irreversibleOutsideTree matches bash whose effect cannot be undone from the workspace's own
// history. Deliberately narrower than policy.go's bashDestructive, which is the in-tree set too.
var irreversibleOutsideTree = []*regexp.Regexp{
	regexp.MustCompile(`\bgit\s+push\b[^;&|]*(--force\b|\s-f\b)`), // rewrites a remote others pulled
	regexp.MustCompile(`\b(dd|mkfs(\.\w+)?|fdisk|parted)\b`),      // raw devices and filesystems
	regexp.MustCompile(`>\s*/dev/(sd[a-z]|nvme\d|disk\d)`),
	regexp.MustCompile(`\bshred\b`),
	regexp.MustCompile(`\bgit\s+branch\s+-D\b[^;&|]*\borigin/`), // deleting somebody else's ref
	regexp.MustCompile(`\bhdiutil\s+erase\b`),
	regexp.MustCompile(`\bdiskutil\s+(erase|reformat)\w*\b`),
}

// rmRecursive matches a recursive/forced delete, whose gravity depends entirely on its target.
var rmRecursive = regexp.MustCompile(`\brm\s+(?:-[a-zA-Z]*\s+)*-[a-zA-Z]*[rf][a-zA-Z]*\s+([^;&|]+)`)

// needsCouncilBeforeRunning reports whether cmd reaches outside the workspace irreversibly.
//
// workdir is the tree the run owns. A path inside it is recoverable from git or from the run's own
// record; a path outside it is somebody else's, and magi has nothing to restore it from.
func needsCouncilBeforeRunning(workdir, cmd string, mine func(string) bool) (why string, yes bool) {
	return needsCouncilBeforeRunningSince(workdir, cmd, mine, nil)
}

// needsCouncilBeforeRunningSince is the same question with the workspace as magi first saw it.
// arrival names the files that were here before this session touched anything — the only ones in
// the tree that nobody here can remake — so a build directory this run produced is not one of
// them however many turns ago it appeared. A nil arrival is "cannot say", and then only the
// turn's own record answers.
func needsCouncilBeforeRunningSince(workdir, cmd string, mine func(string) bool, arrival fileIndex) (why string, yes bool) {
	c := strings.TrimSpace(cmd)
	if c == "" {
		return "", false
	}
	for _, re := range irreversibleOutsideTree {
		if m := re.FindString(c); m != "" {
			return strings.TrimSpace(m), true
		}
	}
	// A recursive delete is judged by where it points, not by the flags it carries.
	for _, m := range rmRecursive.FindAllStringSubmatch(c, -1) {
		for _, target := range strings.Fields(m[1]) {
			if strings.HasPrefix(target, "-") {
				continue // still a flag
			}
			if !outsideWorkspace(workdir, target) {
				// In-tree is exempt because git undoes it. Where nothing does — no repository at
				// or above the workspace — the exemption's whole reason is absent, and the same
				// command is what this gate exists to stop while looking like the harmless case.
				//
				// Asked rather than acted on. A regex over command TEXT cannot know what a shell
				// will delete: `echo "rm -rf build" > clean.sh` matches, so does a heredoc, and a
				// quoted path splits on its space. That is survivable for a QUESTION — a wrong
				// guess costs a turn — and it is not survivable for anything that touches files,
				// which is what an earlier version of this did and had to be taken back out.
				// isScratchPath is not asked here, and that is the point of the branch: it
				// exempts the temp area because a path OUT THERE is the run's own rather than
				// somebody else's, which is a question about targets outside the tree. A
				// workspace that happens to live under /tmp is still the workspace, and the only
				// exemption that means anything for a file inside it is that the run made it.
				if recoverableTree(workdir) || (mine != nil && mine(absTarget(workdir, target))) {
					continue
				}
				// Nothing that was here when magi arrived is under this path, so there is nothing
				// here that this session cannot make again. That is the question the turn-scoped
				// record could not answer: `make` writes build/ in one turn and `rm -rf build`
				// comes in the next, by which time the turn that made it is over.
				if arrival != nil && !arrivalHolds(workdir, target, arrival) {
					continue
				}
				// Nothing there is nothing to lose. `rm -rf build` in a tree that has no build
				// is the ordinary shape of a cleanup step, and asking about it spends a council
				// call and a turn to protect a path that does not exist.
				//
				// A glob is the exception, and the important one: the shell expands it and this
				// cannot, so `rm -rf *` reads as a literal `*` that is never there. That is the
				// command with the most to lose in a tree with no history, so an unexpandable
				// target is treated as present rather than absent.
				// The set is "can the shell rewrite this token", which is larger than the set of
				// glob characters: a brace expansion, a backslash escape, a tilde and a dollar
				// all arrive here as text that names something else after expansion, and a
				// quoted path arrives split. Lstat cannot find any of them, and reading that as
				// "not there" is the failure that fails OPEN — measured on `rm -rf {build,dist}`
				// and `rm -rf "my dir"`, both of which delete and neither of which was asked
				// about.
				if !strings.ContainsAny(target, "*?[{}\\~$`\"'") {
					if _, err := os.Lstat(absTarget(workdir, target)); err != nil {
						continue
					}
				}
				return "rm -rf " + target + " (this workspace has no git history to restore it from)", true
			}
			// Outside the tree is where the gate's premise lives -- "somebody else's, and magi
			// has nothing to restore it from". Two kinds of path are outside and are still
			// nobody else's, and gating them buys nothing while costing the run a turn.
			if isScratchPath(workdir, target) || (mine != nil && mine(absTarget(workdir, target))) {
				continue
			}
			return "rm -rf " + target, true
		}
	}
	return "", false
}

// absTarget resolves a delete target the way outsideWorkspace does, so the two agree on what
// path is being talked about.
func absTarget(workdir, target string) string {
	t := strings.Trim(target, `"'`)
	if !filepath.IsAbs(t) {
		t = filepath.Join(workdir, t)
	}
	return filepath.Clean(t)
}

// isScratchPath reports whether a target lives in the system temp area.
//
// The gate refuses what it cannot restore, on the reading that a path outside the tree belongs to
// somebody else. The temp dir is the one place where that reading is false by convention: it is
// what the OS itself promises nothing about, and a task container's /tmp holds the run's own
// working files and nothing anyone will miss. Measured over 25 refusals in the 2026-08-26 sweep,
// 18 were the agent clearing its own scratch (`rm -rf /tmp/testenv`, `/tmp/test-clone`,
// `/tmp/verify1_cobol`) -- each cost a council round and a refusal the run then had to work around.
//
// The temp root ITSELF is not scratch: `rm -rf /tmp` is a different act from `rm -rf /tmp/mine`,
// and the gate keeps its hold on the former.
func isScratchPath(workdir, target string) bool {
	abs := absTarget(workdir, target)
	for _, root := range scratchRoots() {
		if abs == root {
			return false // the whole temp area, not one thing inside it
		}
		if rel, err := filepath.Rel(root, abs); err == nil && rel != ".." &&
			!strings.HasPrefix(rel, "../") && rel != "." {
			return true
		}
	}
	return false
}

// scratchRoots names the temp areas, TMPDIR included so a run with its own temp is covered.
func scratchRoots() []string {
	roots := []string{"/tmp", "/var/tmp"}
	if t := strings.TrimSpace(os.Getenv("TMPDIR")); t != "" {
		roots = append(roots, filepath.Clean(t))
	}
	return roots
}

// outsideWorkspace reports whether a delete target can leave the workspace.
//
// Unresolvable is treated as outside. A target carrying a variable or a command substitution
// (`$TMP`, `$(cat p)`) is one this cannot evaluate, and the safe reading of "I do not know where
// this points" is not "probably fine" — that is the assumption the whole file exists to avoid.
func outsideWorkspace(workdir, target string) bool {
	t := strings.Trim(target, `"'`)
	if t == "" {
		return false
	}
	if strings.ContainsAny(t, "$`") {
		return true // a variable or a substitution: unresolvable, so not vouched for
	}
	// A glob is unresolvable too, but WHERE it can land is not: one with no separator and no
	// leading `..` is expanded by the shell in the workspace's own directory, so every match it
	// can have is in the tree. `rm -rf *.gcov` and `rm -rf char_*.png` were both refused as
	// "outside" on the 2026-08-26 sweep, and neither could have reached past the cwd.
	if strings.ContainsAny(t, "*?[") {
		if !strings.Contains(t, "/") && !strings.HasPrefix(t, "..") {
			return false
		}
		return true
	}
	if t == "~" || strings.HasPrefix(t, "~/") {
		return true // home is not the workspace even when the workspace is under it
	}
	abs := t
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(workdir, t)
	}
	rel, err := filepath.Rel(workdir, filepath.Clean(abs))
	return err != nil || rel == ".." || strings.HasPrefix(rel, "../")
}

// arrivalHolds reports whether anything the workspace held on arrival lives at or under target.
//
// At OR UNDER, because a delete takes a tree: `rm -rf src` is about every file below src, and a
// directory that held one of the person's files on arrival is not this run's to remove even if
// everything else in it was generated since.
func arrivalHolds(workdir, target string, arrival fileIndex) bool {
	// Both sides unresolved, because that is what the index is keyed by: indexWorkspace walks the
	// workdir it was handed and stores paths relative to THAT, so resolving one side here made
	// every target look like it was outside the tree — and "outside" reads as "holds something",
	// which gated everything.
	abs := absTarget(workdir, target)
	rel, err := filepath.Rel(filepath.Clean(workdir), abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
		return true // cannot place it; the safe reading is that it holds something
	}
	if rel == "." {
		return len(arrival) > 0
	}
	for p := range arrival {
		if p == rel || strings.HasPrefix(p, rel+"/") {
			return true
		}
	}
	return false
}

// gateIrreversible asks the council before a command that cannot be undone, and only where no
// person will be asked. Returns true to stop the call.
//
// Three conditions, each of which removes a reason to run it:
//
//   - a bash call reaching outside the tree irreversibly (needsCouncilBeforeRunning),
//   - nobody watching (`!Interactive`) — an attended run already gets the forced prompt, and asking
//     twice trains people to click through,
//   - a council configured. With `[council] enabled = false` there is nothing to ask, and inventing
//     a refusal because the operator turned the council off would be deciding for them.
func (a *App) gateIrreversible(ctx context.Context, s session.Session, actor event.Actor,
	tc *session.ToolCall, guard *runGuard, toolMsgID string) bool {
	if tc.Name != "bash" || a.cfg.Interactive || a.cfg.Council == nil {
		return false
	}
	var ba struct {
		Command string `json:"command"`
	}
	if json.Unmarshal(tc.Args, &ba) != nil || ba.Command == "" {
		return false
	}
	var mine func(string) bool
	if guard != nil {
		mine = guard.didCreate
	}
	a.mu.Lock()
	arrival := a.stateLocked(s.ID).arrival
	a.mu.Unlock()
	what, yes := needsCouncilBeforeRunningSince(s.Workdir, ba.Command, mine, arrival)
	if !yes {
		return false
	}
	q := fmt.Sprintf(
		"About to run a command whose effect cannot be undone from this workspace: %s\n\nFull command: %s\n\n"+
			"Does the task actually require this, now, in this form? Answer for THIS task only. "+
			"Say yes if the task asked for it or it is the ordinary way to do what was asked; say no "+
			"if it is broader than the task needs, points somewhere the task never mentioned, or "+
			"could be done in a way that leaves a way back.", what, strings.TrimSpace(ba.Command))
	// Asked as ADVICE, not as a deliberation. The question is a plain yes/no about scope and the
	// answer is read as prose (councilSaysNo below), so routing it through the verdict machinery
	// gave the reader a 20 KB instruction to answer in JSON about a TURN — and a reader that
	// answered the question as asked then failed the verdict parse, cost a second full panel
	// prompt on the retry, and reached this line only because that retry happened to parse.
	// Measured on cobol-modernization, 2026-08-23.
	members, _ := a.councilParams()
	advice, err := a.cfg.Council.Advise(ctx, port.AdviceRequest{
		Task:         a.gateTaskText(ctx, s.ID),
		Question:     q,
		Members:      members,
		DefaultModel: s.Model.Model,
	})
	if err != nil {
		// The council is the only reader here and it did not answer. Running anyway would make the
		// gate decorative, so this refuses and says why — the agent can do the recoverable version
		// or hand the decision back.
		a.appendToolResult(ctx, s.ID, actor, toolMsgID, tc.CallID,
			"this command cannot be undone from this workspace ("+what+") and the council could not "+
				"be reached to review it: "+err.Error()+". Do it in a form that leaves a way back, or "+
				"report what you need and stop.", true)
		return true
	}
	if councilSaysNo(advice) {
		a.appendToolResult(ctx, s.ID, actor, toolMsgID, tc.CallID,
			"refused before running: this cannot be undone from this workspace ("+what+"), nobody is "+
				"watching this run, and the council read the task and did not agree it needs this.\n\n"+
				advice, true)
		return true
	}
	return false
}

// councilSaysNo reads a refusal out of the council's prose.
//
// The advice path returns text rather than a verdict — it is the same call the agent makes with
// `council{question}` — so this looks for a refusal and treats everything else as assent. That
// asymmetry is deliberate: an unreadable answer must not become a block, because a gate that fails
// closed on its own parser stops work for a reason nobody can act on.
func councilSaysNo(advice string) bool {
	l := strings.ToLower(advice)
	for _, no := range []string{"no —", "no.", "no,", "do not", "don't", "should not",
		"unnecessary", "broader than", "not required", "not needed"} {
		if strings.Contains(l, no) {
			return true
		}
	}
	return false
}

// gateTaskText is the goal this gate judges scope against: the last user prompt, or the turn's
// re-anchored goal when there is one. Same two sources councilAdvice uses, and for the same reason
// — a re-anchor masks its own prompt, so reading only the log would judge against a stale goal.
func (a *App) gateTaskText(ctx context.Context, sid session.SessionID) string {
	if live := strings.TrimSpace(a.turnTaskNow(sid)); live != "" {
		return live
	}
	evs, _ := a.store.Read(ctx, sid, 0)
	return lastUserPromptText(evs)
}
