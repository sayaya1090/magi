package codingworkspace

import (
	"context"
	task "github.com/sayaya1090/magi/internal/core/codingtask"
	"path/filepath"
	"testing"
	"time"
)

func TestConcurrentTasksCannotOverwriteOneAnother(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	m := New(t.TempDir())
	put(t, filepath.Join(source, "a"), "baseline")
	first, e := m.Prepare(ctx, source, task.Request{Mode: "copy", RequestID: "first"})
	if e != nil {
		t.Fatal(e)
	}
	second, e := m.Prepare(ctx, source, task.Request{Mode: "copy", RequestID: "second"})
	if e != nil {
		t.Fatal(e)
	}
	put(t, filepath.Join(first.Workspace, "a"), "first")
	put(t, filepath.Join(second.Workspace, "a"), "second")
	type outcome struct {
		j task.Journal
		e error
	}
	done := make(chan outcome, 2)
	start := make(chan struct{})
	for _, r := range []task.Record{first, second} {
		go func() { <-start; j, e := m.Apply(ctx, source, r.ID, []string{"a"}); done <- outcome{j, e} }()
	}
	close(start)
	a, b := <-done, <-done
	if a.e != nil || b.e != nil {
		t.Fatalf("%v %v", a.e, b.e)
	}
	if a.j.State == "partial" {
		a, b = b, a
	}
	if a.j.State != "applied" || b.j.State != "partial" {
		t.Fatalf("expected one winner: %+v %+v", a.j, b.j)
	}
	want := "first"
	if a.j.TaskID == second.ID {
		want = "second"
	}
	if got := contents(t, filepath.Join(source, "a")); got != want {
		t.Fatalf("winner %q, contents %q", want, got)
	}
	// Restoring the losing task must not undo the winning task's write.
	if _, e = m.Restore(ctx, source, b.j.TaskID); e != nil {
		t.Fatal(e)
	}
	if got := contents(t, filepath.Join(source, "a")); got != want {
		t.Fatal("loser restored winner's data", got)
	}
	if _, e = m.Restore(ctx, source, a.j.TaskID); e != nil {
		t.Fatal(e)
	}
	if contents(t, filepath.Join(source, "a")) != "baseline" {
		t.Fatal("winner baseline not restored")
	}
}

func TestConcurrentApplySameTaskHasOneJournal(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	m := New(t.TempDir())
	put(t, filepath.Join(source, "a"), "baseline")
	r, e := m.Prepare(ctx, source, task.Request{Mode: "copy"})
	if e != nil {
		t.Fatal(e)
	}
	put(t, filepath.Join(r.Workspace, "a"), "task")
	done := make(chan error, 2)
	start := make(chan struct{})
	for range 2 {
		go func() { <-start; _, e := m.Apply(ctx, source, r.ID, []string{"a"}); done <- e }()
	}
	close(start)
	a, b := <-done, <-done
	if (a == nil) == (b == nil) {
		t.Fatalf("expected one application and one repeat rejection: %v %v", a, b)
	}
	if _, e = m.Restore(ctx, source, r.ID); e != nil {
		t.Fatal(e)
	}
	if contents(t, filepath.Join(source, "a")) != "baseline" {
		t.Fatal("duplicate apply lost baseline")
	}
}

func TestRunLeaseBlocksConcurrentApplyAndRestore(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	m := New(t.TempDir())
	put(t, filepath.Join(source, "a"), "baseline")
	r, e := m.Prepare(ctx, source, task.Request{Mode: "copy"})
	if e != nil {
		t.Fatal(e)
	}
	started, stop, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		_, e := m.Run(ctx, source, r.ID, func(context.Context, string) error { close(started); <-stop; return nil })
		done <- e
	}()
	<-started
	// Always release the callback, even if a regression makes an assertion fail.
	defer func() {
		close(stop)
		if e := <-done; e != nil {
			t.Error(e)
		}
	}()
	for _, operation := range []string{"apply", "restore"} {
		waiting, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		if operation == "apply" {
			_, e = m.Apply(waiting, source, r.ID, []string{"a"})
		} else {
			_, e = m.Restore(waiting, source, r.ID)
		}
		cancel()
		if e == nil || waiting.Err() != context.DeadlineExceeded {
			t.Fatalf("%s ignored active writer lease: %v", operation, e)
		}
	}
	if contents(t, filepath.Join(source, "a")) != "baseline" {
		t.Fatal("active task changed source")
	}
}

func TestForeignSourceDoesNotWaitForActiveTaskLease(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	m := New(t.TempDir())
	put(t, filepath.Join(source, "a"), "baseline")
	r, e := m.Prepare(ctx, source, task.Request{Mode: "copy"})
	if e != nil {
		t.Fatal(e)
	}
	unlock, e := m.lock(ctx, r.ID)
	if e != nil {
		t.Fatal(e)
	}
	defer unlock()
	wrongSource := t.TempDir()
	for _, operation := range []struct {
		name string
		call func(context.Context) error
	}{
		{"review", func(c context.Context) error { _, e := m.Review(c, wrongSource, r.ID, "a"); return e }},
		{"diff", func(c context.Context) error { _, e := m.Diff(c, wrongSource, r.ID); return e }},
		{"apply", func(c context.Context) error { _, e := m.Apply(c, wrongSource, r.ID, []string{"a"}); return e }},
		{"restore", func(c context.Context) error { _, e := m.Restore(c, wrongSource, r.ID); return e }},
		{"recover", func(c context.Context) error { _, e := m.Recover(c, wrongSource, r.ID); return e }},
		{"run", func(c context.Context) error {
			_, e := m.Run(c, wrongSource, r.ID, func(context.Context, string) error { t.Error("foreign run callback called"); return nil })
			return e
		}},
	} {
		t.Run(operation.name, func(t *testing.T) {
			waiting, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
			defer cancel()
			err := operation.call(waiting)
			if err == nil {
				t.Fatal("foreign source accepted")
			}
			if waiting.Err() != nil {
				t.Fatalf("foreign source waited for task lease instead of being rejected: %v", err)
			}
		})
	}
}
