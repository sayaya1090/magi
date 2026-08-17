package app

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
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
func needsCouncilBeforeRunning(workdir, cmd string) (why string, yes bool) {
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
			if outsideWorkspace(workdir, target) {
				return "rm -rf " + target, true
			}
		}
	}
	return "", false
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
	if strings.ContainsAny(t, "$`*?") {
		return true // a variable, a substitution or a glob: unresolvable, so not vouched for
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
	what, yes := needsCouncilBeforeRunning(s.Workdir, ba.Command)
	if !yes {
		return false
	}
	q := fmt.Sprintf(
		"About to run a command whose effect cannot be undone from this workspace: %s\n\nFull command: %s\n\n"+
			"Does the task actually require this, now, in this form? Answer for THIS task only. "+
			"Say yes if the task asked for it or it is the ordinary way to do what was asked; say no "+
			"if it is broader than the task needs, points somewhere the task never mentioned, or "+
			"could be done in a way that leaves a way back.", what, strings.TrimSpace(ba.Command))
	advice, err := a.councilAdvice(ctx, s, guardChanges(guard), guardEpoch(guard), q, false)
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
