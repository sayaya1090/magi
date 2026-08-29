package app

import (
	"context"
	"encoding/json"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The write-approval diff is taken against the file as it is, so a one-line addition reads as one
// added line beside its context — not as the whole file added (the all-`+` view live-QA could not
// find the change in).
func TestWriteApprovalDiffIsAgainstTheRealFile(t *testing.T) {
	wd := t.TempDir()
	if err := os.WriteFile(filepath.Join(wd, "a.txt"), []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]string{"path": "a.txt", "content": "one\ntwo\nthree\n"})
	diff, ok := writeApprovalDiff(wd, args)
	if !ok {
		t.Fatal("an existing readable file must yield an authoritative diff")
	}
	if !strings.Contains(diff, " one") || !strings.Contains(diff, "+three") {
		t.Fatalf("the diff must show context and the one addition, got %q", diff)
	}
	if strings.Contains(diff, "+one") {
		t.Fatalf("an unchanged line must not read as added, got %q", diff)
	}
}

func TestWriteApprovalDiffFallsBackHonestly(t *testing.T) {
	wd := t.TempDir()
	fresh, _ := json.Marshal(map[string]string{"path": "new.txt", "content": "hello\n"})
	if _, ok := writeApprovalDiff(wd, fresh); ok {
		t.Error("a file that does not exist yet keeps the args view — all additions IS its truth")
	}
	escape, _ := json.Marshal(map[string]string{"path": "../outside.txt", "content": "x"})
	if _, ok := writeApprovalDiff(wd, escape); ok {
		t.Error("a path that leaves the workdir must not be read for a preview")
	}
	if err := os.WriteFile(filepath.Join(wd, "same.txt"), []byte("keep\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	noop, _ := json.Marshal(map[string]string{"path": "same.txt", "content": "keep\n"})
	diff, ok := writeApprovalDiff(wd, noop)
	if !ok || !strings.Contains(diff, "no change") {
		t.Errorf("an identical rewrite says so in words — omitempty wire fields cannot carry an empty authoritative answer — got ok=%v diff=%q", ok, diff)
	}
}

// Truncation is the most destructive write there is, and empty content is how it looks: the
// preview shows every line removed, not the raw args.
func TestWriteApprovalDiffShowsATruncationAsRemovals(t *testing.T) {
	wd := t.TempDir()
	if err := os.WriteFile(filepath.Join(wd, "gone.txt"), []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]string{"path": "gone.txt"})
	diff, ok := writeApprovalDiff(wd, args)
	if !ok || !strings.Contains(diff, "-one") || !strings.Contains(diff, "-two") {
		t.Fatalf("emptying a file must preview as removals, got ok=%v diff=%q", ok, diff)
	}
}

// A symlink inside the workdir pointing outside is exactly what the real jail refuses; the
// preview must refuse it too, or it reads jail-refused bytes onto approval screens for a write
// that cannot happen.
func TestWriteApprovalDiffRefusesASymlinkOutOfTheJail(t *testing.T) {
	wd, outside := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("s3cret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(wd, "link")); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]string{"path": "link/secret", "content": "x"})
	if diff, ok := writeApprovalDiff(wd, args); ok {
		t.Fatalf("a symlink out of the workdir must not be read for a preview, got %q", diff)
	}
}

// The prompt actually carries the real-file diff: drop the override in requestPermission and this
// fails, which is the point — the unit tests above cannot see that seam.
func TestTheAskCarriesTheRealFileDiff(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{Permission: "ask", Interactive: true, AnswerWait: 5 * time.Second})
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
	if err := os.WriteFile(filepath.Join(wd, "a.txt"), []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tc := &session.ToolCall{CallID: "c1", Name: "write", Args: json.RawMessage(`{"path":"a.txt","content":"one\ntwo\nthree\n"}`)}
	got := make(chan bool, 1)
	go func() {
		got <- a.requestPermission(context.Background(), sid, event.Actor{Kind: event.ActorUser, ID: "u"}, tc, true, "")
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if ask, ok := a.Waiting(sid); ok {
			if !strings.Contains(ask.Diff, "+three") || strings.Contains(ask.Diff, "+one") {
				t.Errorf("the ask must carry the real-file diff, got %q", ask.Diff)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the prompt never registered")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := a.RespondPermission(context.Background(), command.RespondPermission{SessionID: sid, CallID: "c1", Decision: "deny"}); err != nil {
		t.Fatal(err)
	}
	<-got
}
