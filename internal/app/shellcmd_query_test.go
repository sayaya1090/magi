package app

import "testing"

// mutatesFiles decides whether a command authored anything, and a "yes" restarts the no-progress
// window. It matched on the verb and ONE subcommand, so a third token that turns the subcommand
// back into a question went unread — and asking a question bought a fresh window.
//
// Live: `git stash list 2>&1 || echo "not a git repo"`, run in a container with no `.git` by an
// agent hunting for edits it had lost. It wrote nothing, it could write nothing, and it reset the
// counter.
func TestAQueryingSubcommandIsNotAMutation(t *testing.T) {
	for _, cmd := range []string{
		`cd /app/ocaml && git stash list 2>&1 || echo "not a git repo"`,
		`git stash list`,
		`git stash show -p`,
	} {
		if mutatesFiles(cmd) {
			t.Errorf("%q only asks what is stashed — it mutates nothing", cmd)
		}
	}
	// The mutating forms must still read as mutations: a too-eager exemption would hide real work.
	for _, cmd := range []string{
		`git stash`,
		`git stash push -m wip`,
		`git stash pop`,
		`cd /app && git apply fix.patch`,
		`git checkout -- .`,
	} {
		if !mutatesFiles(cmd) {
			t.Errorf("%q changes the worktree", cmd)
		}
	}
	// A verb with no exemptions at all is unaffected by the lookup.
	if !mutatesFiles(`pip install requests`) {
		t.Error("pip install still mutates the environment")
	}
	if mutatesFiles(`pip list`) {
		t.Error("pip list was never a mutation")
	}
}
