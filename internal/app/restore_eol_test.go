package app

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
)

// The restore hands back what git would check out — including the tree's own line-ending rule.
//
// `core.autocrlf` is on by default in the Git for Windows installer, and under it a checkout
// rewrites LF to CRLF. So a file committed with `\n` comes back with `\r\n`, and the sentence
// "git can still put a tracked file back exactly" was not true there. Measured 2026-09-11.
//
// ⚠ **This test exists to stop that being "fixed".** The obvious repair is for the restore to pass
// `-c core.autocrlf=false`, and it would be wrong: the setting is the person's, it governs every
// other checkout in that tree, and a restore that alone ignored it would put back a file their next
// `git status` calls modified. What was wrong was the word, not the behaviour.
//
// So this pins the behaviour and the report together: the bytes are the tree's, and the outcome
// still says it was restored and by what.
func TestARestoreFollowsTheTreesOwnLineEndingRule(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on this machine")
	}
	for _, c := range []struct {
		name, autocrlf, want string
	}{
		{"a tree that does not convert", "false", "committed\n"},
		{"a tree that converts", "true", "committed\r\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			a, dir := restoreRepo(t, c.autocrlf)
			sid := session.SessionID("eol-child")
			a.journalFor(sid, dir).note("tracked.txt", "", false, true)
			write(t, dir, "tracked.txt", "the child's version\n")

			out := a.RestoreChild(context.Background(), sid)
			if got := read(t, dir, "tracked.txt"); got != c.want {
				t.Errorf("autocrlf=%s 인 트리에서 %q 로 돌아왔다, 원하는 것 %q", c.autocrlf, got, c.want)
			}
			// Whatever the bytes, the report says it happened and by which route — a restore that
			// silently did nothing must not look like this one.
			if r := outcomeFor(t, out, "tracked.txt"); !r.Restored || r.How != "git" {
				t.Errorf("보고가 %q (restored=%v), git 이어야 한다", r.How, r.Restored)
			}
		})
	}
}

// restoreRepo builds a repository with one committed file and a chosen line-ending rule.
func restoreRepo(t *testing.T, autocrlf string) (*App, string) {
	t.Helper()
	a, dir := restoreApp(t)
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	git("config", "user.email", "t@example.com")
	git("config", "user.name", "t")
	// The whole point of this fixture: the rule is chosen here rather than inherited from whatever
	// git the test happens to run under.
	git("config", "core.autocrlf", autocrlf)
	if autocrlf == "false" {
		git("config", "core.eol", "lf")
	}
	write(t, dir, "tracked.txt", "committed\n")
	git("add", "tracked.txt")
	// A conversion warning on `add` is normal and not a failure; CombinedOutput only fails on a
	// non-zero exit, so nothing here needs to read it.
	git("commit", "-qm", "base")
	if got := read(t, dir, "tracked.txt"); autocrlf == "false" && strings.Contains(got, "\r") {
		t.Fatalf("픽스처가 이미 변환됐다: %q — 이 시험의 전제가 무너졌다", got)
	}
	return a, dir
}
