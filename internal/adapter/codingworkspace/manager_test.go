package codingworkspace

import (
	"context"
	task "github.com/sayaya1090/magi/internal/core/codingtask"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func put(t *testing.T, p, s string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(s), 0600); err != nil {
		t.Fatal(err)
	}
}
func contents(t *testing.T, p string) string {
	t.Helper()
	b, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}
func TestCopyApplyRestartRestoreAndConflict(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	store := t.TempDir()
	m := New(store)
	put(t, filepath.Join(source, "a"), "original")
	put(t, filepath.Join(source, "b"), "baseline")
	put(t, filepath.Join(source, ".env"), "secret")
	r, e := m.Prepare(ctx, source, task.Request{RequestID: "test", Mode: "copy"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Join(r.Workspace, ".env")); !os.IsNotExist(e) {
		t.Fatal("secret copied")
	}
	put(t, filepath.Join(r.Workspace, "a"), "task")
	put(t, filepath.Join(r.Workspace, "b"), "task")
	put(t, filepath.Join(r.Workspace, "new"), "created")
	if contents(t, filepath.Join(source, "a")) != "original" {
		t.Fatal("copy shares writes")
	}
	put(t, filepath.Join(source, "b"), "user edit")
	j, e := m.Apply(ctx, source, r.ID, []string{"a", "b", "new"})
	if e != nil {
		t.Fatal(e)
	}
	if j.State != "partial" {
		t.Fatalf("%+v", j)
	}
	if contents(t, filepath.Join(source, "a")) != "task" || contents(t, filepath.Join(source, "b")) != "user edit" {
		t.Fatal("apply conflict guard")
	}
	m = New(store)
	put(t, filepath.Join(source, "new"), "later edit")
	j, e = m.Restore(ctx, source, r.ID)
	if e != nil {
		t.Fatal(e)
	}
	if j.State != "partial" {
		t.Fatal(j.State)
	}
	if j.ApplyState != "partial" || len(j.ApplyOutcomes) != 3 {
		t.Fatalf("apply audit lost: %+v", j)
	}
	if contents(t, filepath.Join(source, "a")) != "original" || contents(t, filepath.Join(source, "new")) != "later edit" {
		t.Fatal("restore lost user data")
	}
	if _, e := os.Stat(filepath.Join(source, ".git")); !os.IsNotExist(e) {
		t.Fatal("initialized git")
	}
}
func TestGitIsolationAndRetry(t *testing.T) {
	if _, e := exec.LookPath("git"); e != nil {
		t.Skip("Git not installed")
	}
	ctx := context.Background()
	source := t.TempDir()
	m := New(t.TempDir())
	for _, args := range [][]string{{"init"}, {"config", "user.email", "test@example.invalid"}, {"config", "user.name", "test"}} {
		if _, e := git(ctx, source, args...); e != nil {
			t.Fatal(e)
		}
	}
	put(t, filepath.Join(source, "a"), "original")
	if _, e := git(ctx, source, "add", "a"); e != nil {
		t.Fatal(e)
	}
	if _, e := git(ctx, source, "commit", "-m", "initial"); e != nil {
		t.Fatal(e)
	}
	q := task.Request{RequestID: "git-test"}
	r, e := m.Prepare(ctx, source, q)
	if e != nil {
		t.Fatal(e)
	}
	again, e := m.Prepare(ctx, source, q)
	if e != nil || again.ID != r.ID {
		t.Fatalf("retry: %v", e)
	}
	put(t, filepath.Join(r.Workspace, "a"), "task")
	if contents(t, filepath.Join(source, "a")) != "original" {
		t.Fatal("source modified")
	}
	diff, e := m.Diff(ctx, source, r.ID)
	if e != nil || len(diff) != 1 {
		t.Fatalf("diff=%v err=%v", diff, e)
	}
	if _, e := m.Apply(ctx, source, r.ID, []string{"a"}); e == nil {
		t.Fatal("git applied without review")
	}
}
func TestSymlinkAndBudgetFailClosed(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	put(t, filepath.Join(source, "a"), "large")
	m := New(t.TempDir())
	m.MaxBytes = 1
	if _, e := m.Prepare(ctx, source, task.Request{Mode: "copy"}); e == nil {
		t.Fatal("budget ignored")
	}
	if e := os.Symlink(t.TempDir(), filepath.Join(source, "link")); e != nil {
		t.Skip(e)
	}
	m = New(t.TempDir())
	if _, e := m.Prepare(ctx, source, task.Request{Mode: "copy"}); e == nil {
		t.Fatal("symlink accepted")
	}
	for _, p := range []string{"../outside", "/absolute", "a/../../b", ".git/config"} {
		if _, e := target(source, p); e == nil {
			t.Fatal(p)
		}
	}
	if e := m.writeFile(t.TempDir(), source, "a", &task.File{Digest: "../../outside"}); e == nil {
		t.Fatal("invalid digest accepted")
	}
}

func TestStorageAliasStableAcrossRestart(t *testing.T) {
	parent := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if e := os.Symlink(parent, alias); e != nil {
		t.Skip(e)
	}
	store := filepath.Join(alias, "not-created", "tasks")
	source := t.TempDir()
	put(t, filepath.Join(source, "a"), "value")
	r, e := New(store).Prepare(context.Background(), source, task.Request{Mode: "copy"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = New(store).Status(context.Background(), source, r.ID); e != nil {
		t.Fatal(e)
	}
}

func TestRejectedStorageDoesNotModifySource(t *testing.T) {
	source := t.TempDir()
	store := filepath.Join(source, "nested", "tasks")
	if _, e := New(store).Prepare(context.Background(), source, task.Request{Mode: "copy"}); e == nil {
		t.Fatal("nested store accepted")
	}
	if _, e := os.Stat(filepath.Join(source, "nested")); !os.IsNotExist(e) {
		t.Fatal("rejected request changed source")
	}
}
func TestInterruptedRunRecoveryAndCancelledLease(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	m := New(t.TempDir())
	put(t, filepath.Join(source, "a"), "before")
	r, e := m.Prepare(ctx, source, task.Request{Mode: "copy"})
	if e != nil {
		t.Fatal(e)
	}
	// Simulate process death after the durable running intent.
	r.State = "running"
	if e = m.write(&r); e != nil {
		t.Fatal(e)
	}
	r, e = m.Recover(ctx, source, r.ID)
	if e != nil || r.State != "ready" {
		t.Fatalf("%+v %v", r, e)
	}
	unlock, e := m.lock(ctx, r.ID)
	if e != nil {
		t.Fatal(e)
	}
	defer unlock()
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, e = m.Recover(cancelled, source, r.ID); e == nil {
		t.Fatal("cancelled lease succeeded")
	}
}
func TestDamagedBackupFailsBeforeSourceMutation(t *testing.T) {
	source := t.TempDir()
	m := New(t.TempDir())
	ctx := context.Background()
	put(t, filepath.Join(source, "a"), "before")
	r, e := m.Prepare(ctx, source, task.Request{Mode: "copy"})
	if e != nil {
		t.Fatal(e)
	}
	put(t, filepath.Join(r.Workspace, "a"), "after")
	dir, _ := m.dir(r.ID)
	if e = os.Remove(filepath.Join(dir, "blobs", r.Baseline["a"].Digest)); e != nil {
		t.Fatal(e)
	}
	if _, e = m.Apply(ctx, source, r.ID, []string{"a"}); e == nil {
		t.Fatal("missing backup accepted")
	}
	if contents(t, filepath.Join(source, "a")) != "before" {
		t.Fatal("source changed without backup")
	}
}

func TestConcurrentPreparationHasOneTask(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	m := New(t.TempDir())
	put(t, filepath.Join(source, "a"), "data")
	type result struct {
		r task.Record
		e error
	}
	done := make(chan result, 2)
	for range 2 {
		go func() {
			r, e := m.Prepare(ctx, source, task.Request{Mode: "copy", RequestID: "same"})
			done <- result{r, e}
		}()
	}
	a, b := <-done, <-done
	if a.e != nil || b.e != nil || a.r.ID != b.r.ID {
		t.Fatalf("%+v %+v", a, b)
	}
	entries, e := m.List(ctx, source)
	if e != nil || len(entries) != 1 {
		t.Fatalf("entries=%d error=%v", len(entries), e)
	}
}
func TestDeletionRestore(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	m := New(t.TempDir())
	put(t, filepath.Join(source, "a"), "before")
	r, e := m.Prepare(ctx, source, task.Request{Mode: "copy"})
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Remove(filepath.Join(r.Workspace, "a")); e != nil {
		t.Fatal(e)
	}
	if _, e = m.Apply(ctx, source, r.ID, []string{"a"}); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(source, "a")); !os.IsNotExist(e) {
		t.Fatal("not deleted")
	}
	if _, e = m.Restore(ctx, source, r.ID); e != nil {
		t.Fatal(e)
	}
	if contents(t, filepath.Join(source, "a")) != "before" {
		t.Fatal("deletion not restored")
	}
}

func TestRestoreInterruptedWriteWithoutCompletionRecord(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	m := New(t.TempDir())
	put(t, filepath.Join(source, "a"), "before")
	r, e := m.Prepare(ctx, source, task.Request{Mode: "copy"})
	if e != nil {
		t.Fatal(e)
	}
	put(t, filepath.Join(r.Workspace, "a"), "after")
	diff, e := m.Diff(ctx, source, r.ID)
	if e != nil {
		t.Fatal(e)
	}
	dir, _ := m.dir(r.ID)
	j := task.Journal{ID: nonce(), TaskID: r.ID, Kind: "apply", State: "prepared", Changes: diff}
	if e = save(filepath.Join(dir, "integration.json"), j); e != nil {
		t.Fatal(e)
	}
	// A crash after the write but before its outcome is saved leaves only intent.
	put(t, filepath.Join(source, "a"), "after")
	j, e = New(m.Root).Restore(ctx, source, r.ID)
	if e != nil || j.State != "restored" {
		t.Fatalf("%+v %v", j, e)
	}
	if contents(t, filepath.Join(source, "a")) != "before" {
		t.Fatal("unrecorded write not restored")
	}
}

func TestReviewExactTextAndBinaryOmission(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	m := New(t.TempDir())
	put(t, filepath.Join(source, "a"), " before\n")
	r, e := m.Prepare(ctx, source, task.Request{Mode: "copy"})
	if e != nil {
		t.Fatal(e)
	}
	put(t, filepath.Join(r.Workspace, "a"), " after\n")
	review, e := m.Review(ctx, source, r.ID, "a")
	if e != nil {
		t.Fatal(e)
	}
	if review.BeforeText == nil || *review.BeforeText != " before\n" || review.AfterText == nil || *review.AfterText != " after\n" {
		t.Fatalf("%+v", review)
	}
	put(t, filepath.Join(r.Workspace, "a"), "binary\x00data")
	review, e = m.Review(ctx, source, r.ID, "a")
	if e != nil || review.Omitted == "" || review.BeforeText != nil || review.AfterText != nil {
		t.Fatalf("%+v %v", review, e)
	}
}

func TestRetainedVersionsHaveBudget(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	m := New(t.TempDir())
	m.MaxStoredBytes = 6
	put(t, filepath.Join(source, "a"), "one")
	r, e := m.Prepare(ctx, source, task.Request{Mode: "copy"})
	if e != nil {
		t.Fatal(e)
	}
	put(t, filepath.Join(r.Workspace, "a"), "two")
	if _, e = m.Diff(ctx, source, r.ID); e != nil {
		t.Fatal(e)
	}
	put(t, filepath.Join(r.Workspace, "a"), "three")
	if _, e = m.Diff(ctx, source, r.ID); e == nil {
		t.Fatal("unbounded retained versions")
	}
	if contents(t, filepath.Join(source, "a")) != "one" {
		t.Fatal("budget failure changed source")
	}
}

func TestWorkspaceRootReplacementRejected(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	m := New(t.TempDir())
	put(t, filepath.Join(source, "a"), "original")
	r, e := m.Prepare(ctx, source, task.Request{Mode: "copy"})
	if e != nil {
		t.Fatal(e)
	}
	moved := r.Workspace + "-preserved"
	if e = os.Rename(r.Workspace, moved); e != nil {
		t.Fatal(e)
	}
	if e = os.Symlink(source, r.Workspace); e != nil {
		t.Skip(e)
	}
	if _, e = m.Diff(ctx, source, r.ID); e == nil {
		t.Fatal("symlink root interpreted as empty workspace")
	}
	if _, e = m.Apply(ctx, source, r.ID, []string{"a"}); e == nil {
		t.Fatal("replaced root applied")
	}
	if contents(t, filepath.Join(source, "a")) != "original" {
		t.Fatal("source changed")
	}
}
func TestNoGitEnvironmentUsesCopy(t *testing.T) {
	source := t.TempDir()
	put(t, filepath.Join(source, "a"), "data")
	t.Setenv("PATH", t.TempDir())
	r, e := New(t.TempDir()).Prepare(context.Background(), source, task.Request{})
	if e != nil || r.Mode != "copy" {
		t.Fatalf("%+v %v", r, e)
	}
}
func TestBrokenGitIsNotClassifiedAsNoGit(t *testing.T) {
	source := t.TempDir()
	put(t, filepath.Join(source, ".git"), "not a valid git directory")
	if _, e := New(t.TempDir()).Prepare(context.Background(), source, task.Request{}); e == nil {
		t.Fatal("broken Git silently downgraded")
	}
	if _, e := New(t.TempDir()).Prepare(context.Background(), source, task.Request{Mode: "copy"}); e != nil {
		t.Fatal("explicit copy should not require Git metadata", e)
	}
}
func TestUnbornGitRequiresExplicitCopy(t *testing.T) {
	if _, e := exec.LookPath("git"); e != nil {
		t.Skip("Git not installed")
	}
	ctx := context.Background()
	source := t.TempDir()
	if _, e := git(ctx, source, "init"); e != nil {
		t.Fatal(e)
	}
	put(t, filepath.Join(source, "a"), "data")
	m := New(t.TempDir())
	if _, e := m.Prepare(ctx, source, task.Request{RequestID: "auto"}); e == nil {
		t.Fatal("unborn repository auto-initialized")
	}
	r, e := m.Prepare(ctx, source, task.Request{RequestID: "copy", Mode: "copy"})
	if e != nil || r.Mode != "copy" {
		t.Fatalf("%+v %v", r, e)
	}
	if _, e = git(ctx, source, "rev-parse", "--verify", "HEAD"); e == nil {
		t.Fatal("unexpected initial commit")
	}
}
