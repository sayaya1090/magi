package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

func TestFileCheckpointCopiesAndPublishesOnlySelectedDocument(t *testing.T) {
	ctx := context.Background()
	a, s, _ := spawnApp(t, &fakeLLM{})
	s.Workdir = t.TempDir()
	a.cfg.StoreDir = t.TempDir()
	os.MkdirAll(filepath.Join(s.Workdir, "docs"), 0700)
	document := filepath.Join(s.Workdir, "docs", "deck.pptx")
	sibling := filepath.Join(s.Workdir, "private.txt")
	os.WriteFile(document, []byte("PK\x03\x04original"), 0600)
	os.WriteFile(sibling, []byte("never copy"), 0600)
	ops := a.checkpointFn(0, s)
	call := func(req port.CheckpointRequest) port.CheckpointResult {
		t.Helper()
		r, err := ops(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	start := call(port.CheckpointRequest{Action: "begin", Kind: "file", Path: "docs/deck.pptx"})
	t.Cleanup(func() { os.RemoveAll(start.Recovery) })
	fork := call(port.CheckpointRequest{Action: "fork"})
	if fork.Resource != filepath.Join(fork.Path, "docs", "deck.pptx") {
		t.Fatalf("missing exact file binding: %+v", fork)
	}
	if _, err := os.Stat(filepath.Join(fork.Path, "private.txt")); !os.IsNotExist(err) {
		t.Fatal("sibling copied")
	}
	os.WriteFile(fork.Resource, []byte("PK\x03\x04candidate"), 0600)
	os.WriteFile(filepath.Join(fork.Path, "private.txt"), []byte("must not publish"), 0600)
	call(port.CheckpointRequest{Action: "adopt", Branch: fork.Branch})
	next := call(port.CheckpointRequest{Action: "fork"})
	if _, err := os.Stat(filepath.Join(next.Path, "private.txt")); !os.IsNotExist(err) {
		t.Fatal("non-resource promoted to next checkpoint")
	}
	// An unrelated user edit does not conflict with a single-document checkpoint.
	os.WriteFile(sibling, []byte("user changed sibling"), 0600)
	call(port.CheckpointRequest{Action: "finish"})
	if err := a.finishCheckpoints(ctx, s.ID); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(document)
	if string(data) != "PK\x03\x04candidate" {
		t.Fatalf("document not published: %s", data)
	}
	data, _ = os.ReadFile(sibling)
	if string(data) != "user changed sibling" {
		t.Fatalf("sibling overwritten: %s", data)
	}
	status := call(port.CheckpointRequest{Action: "status"})
	if status.Phase != "cleaned" {
		t.Fatalf("not cleaned: %+v", status)
	}
}

func TestFileCheckpointRefusesInvalidResources(t *testing.T) {
	for i, path := range []string{"", ".", "../outside.docx", "/tmp/outside.docx", ".git/config", "folder", "link.docx"} {
		t.Run(path, func(t *testing.T) {
			a, s, _ := spawnApp(t, &fakeLLM{})
			s.Workdir = t.TempDir()
			a.cfg.StoreDir = t.TempDir()
			os.MkdirAll(filepath.Join(s.Workdir, "folder"), 0700)
			os.Symlink(filepath.Join(t.TempDir(), "outside"), filepath.Join(s.Workdir, "link.docx"))
			s.ID = session.SessionID(strings.Repeat("s", i+1))
			if _, err := a.checkpointFn(0, s)(context.Background(), port.CheckpointRequest{Action: "begin", Kind: "file", Path: path}); err == nil {
				t.Fatalf("accepted %q", path)
			}
		})
	}
}

func TestFileCheckpointPinsIndependentVerifierAcrossBranches(t *testing.T) {
	ctx := context.Background()
	a, s, _ := spawnApp(t, &fakeLLM{})
	s.Workdir = t.TempDir()
	a.cfg.StoreDir = t.TempDir()
	os.WriteFile(filepath.Join(s.Workdir, "document.docx"), []byte("PK\x03\x04doc"), 0600)
	verifier := filepath.Join(s.Workdir, "verify.sh")
	os.WriteFile(verifier, []byte("exit 1\n"), 0600)
	w, err := a.checkpointFor(ctx, s.Workdir, string(s.ID), "file", "document.docx")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(w.root) })
	var digest string
	for i := 0; i < 2; i++ {
		fork, err := w.action(ctx, port.CheckpointRequest{Action: "fork"})
		if err != nil {
			t.Fatal(err)
		}
		if err := w.prepareFileVerifier(fork.Branch, "verify.sh"); err != nil {
			t.Fatal(err)
		}
		pin, err := readTrialVerifier(fork.Path, "verify.sh")
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			digest = pin.digest
			os.WriteFile(verifier, []byte("exit 0\n"), 0600)
		} else if digest != pin.digest {
			t.Fatal("verifier drifted across runs")
		}
		if err := w.prepareFileVerifier(fork.Branch, "document.docx"); err == nil {
			t.Fatal("resource used as verifier")
		}
	}
}

type checkpointDocumentFixture struct {
	path                      string
	fail                      bool
	opened, closed, confirmed int
}

func (d *checkpointDocumentFixture) OpenCopy(_ context.Context, path string) (port.CheckpointDocument, error) {
	d.path = path
	d.opened++
	return d, nil
}
func (d *checkpointDocumentFixture) SaveAndClose(context.Context) error {
	if d.fail {
		return fmt.Errorf("application has not released the file")
	}
	d.closed++
	return os.WriteFile(d.path, []byte("saved by application"), 0600)
}
func (d *checkpointDocumentFixture) ConfirmClosed(context.Context, string) error {
	d.confirmed++
	if d.fail {
		return fmt.Errorf("still open")
	}
	return nil
}
func TestFileCheckpointRetainsOpenDocumentUntilSaveAndCloseConfirmed(t *testing.T) {
	ctx := context.Background()
	a, s, _ := spawnApp(t, &fakeLLM{})
	s.Workdir = t.TempDir()
	a.cfg.StoreDir = t.TempDir()
	os.WriteFile(filepath.Join(s.Workdir, "document.docx"), []byte("original"), 0600)
	adapter := &checkpointDocumentFixture{fail: true}
	a.cfg.CheckpointDocuments = adapter
	w, err := a.checkpointFor(ctx, s.Workdir, string(s.ID), "file", "document.docx")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(w.root) })
	fork, err := w.action(ctx, port.CheckpointRequest{Action: "fork"})
	if err != nil {
		t.Fatal(err)
	}
	if adapter.path != fork.Resource {
		t.Fatal("adapter opened the wrong document")
	}
	if _, err := w.action(ctx, port.CheckpointRequest{Action: "adopt", Branch: fork.Branch}); err == nil {
		t.Fatal("open document adopted")
	}
	if _, err := os.Stat(fork.Resource); err != nil {
		t.Fatal("open copy deleted")
	}
	adapter.fail = false
	if _, err := w.action(ctx, port.CheckpointRequest{Action: "adopt", Branch: fork.Branch}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.action(ctx, port.CheckpointRequest{Action: "finish"}); err != nil {
		t.Fatal(err)
	}
	if err := a.finishCheckpoints(ctx, s.ID); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(s.Workdir, "document.docx"))
	if string(data) != "saved by application" || adapter.closed != 1 || adapter.confirmed < 1 {
		t.Fatalf("save/close ordering broken: %s %+v", data, adapter)
	}
}
