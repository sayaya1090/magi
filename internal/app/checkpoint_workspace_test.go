package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/port"
)

func TestCheckpointLifecycleWorktreeAndDirectory(t *testing.T) {
	for _, kind := range []string{"git", "directory"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			a, s, _ := spawnApp(t, &fakeLLM{})
			s.Workdir = t.TempDir()
			if kind == "git" {
				s.Workdir = gitRepo(t)
			}
			a.cfg.StoreDir = t.TempDir()
			put := func(dir, name, value string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(dir, name), []byte(value), 0600); err != nil {
					t.Fatal(err)
				}
			}
			put(s.Workdir, "document.txt", "initial")
			ops := a.checkpointFn(0, s)
			call := func(action, branch string) port.CheckpointResult {
				t.Helper()
				r, err := ops(ctx, port.CheckpointRequest{Action: action, Kind: kind, Branch: branch})
				if err != nil {
					t.Fatal(err)
				}
				return r
			}
			start := call("begin", "")
			t.Cleanup(func() { os.RemoveAll(start.Recovery) })
			left, right := call("fork", ""), call("fork", "")
			if kind == "git" {
				data, err := os.ReadFile(filepath.Join(left.Path, ".git"))
				if err != nil || !strings.HasPrefix(string(data), "gitdir:") {
					t.Fatalf("not a git worktree: %s %v", data, err)
				}
			}
			put(left.Path, "document.txt", "discarded")
			put(right.Path, "document.txt", "adopted")
			adopted := call("adopt", right.Branch)
			if adopted.Checkpoint == start.Checkpoint {
				t.Fatal("adoption did not advance checkpoint")
			}
			if _, err := ops(ctx, port.CheckpointRequest{Action: "adopt", Branch: left.Branch}); err == nil {
				t.Fatal("stale sibling adopted")
			}
			next := call("fork", "")
			data, err := os.ReadFile(filepath.Join(next.Path, "document.txt"))
			if err != nil || string(data) != "adopted" {
				t.Fatalf("next branch starts from wrong checkpoint: %s %v", data, err)
			}
			// Mutating the adopted old directory must not mutate the frozen checkpoint.
			put(right.Path, "document.txt", "late mutation")
			put(next.Path, "document.txt", "final")
			put(next.Path, "new.txt", "added")
			call("adopt", next.Branch)
			call("finish", "")
			original, _ := os.ReadFile(filepath.Join(s.Workdir, "document.txt"))
			if string(original) != "initial" {
				t.Fatal("finish published before host completion")
			}
			a.finishCheckpoints(ctx, s.ID)
			result, _ := os.ReadFile(filepath.Join(s.Workdir, "document.txt"))
			if string(result) != "final" {
				t.Fatalf("not published: %s", result)
			}
			status := call("status", "")
			if status.Phase != "cleaned" {
				t.Fatalf("not cleaned: %+v", status)
			}
			for _, path := range []string{left.Path, right.Path, next.Path} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("branch remains: %s %v", path, err)
				}
			}
			if _, err := os.Stat(filepath.Join(start.Recovery, "manifest.json")); err != nil {
				t.Fatal("audit manifest lost", err)
			}
			// Completion is idempotent; original changes after publication survive retries.
			put(s.Workdir, "document.txt", "later user edit")
			a.finishCheckpoints(ctx, s.ID)
			result, _ = os.ReadFile(filepath.Join(s.Workdir, "document.txt"))
			if string(result) != "later user edit" {
				t.Fatal("completion retry overwrote later edit")
			}
			a.mu.Lock()
			a.stateLocked(s.ID).checkpoint = nil
			a.mu.Unlock()
			fresh := call("begin", "")
			if fresh.Phase != "open" || fresh.Ready || fresh.Checkpoint == adopted.Checkpoint {
				t.Fatalf("cannot start next task after restart: %+v", fresh)
			}
		})
	}
}

func TestCheckpointConflictRetainsResultsAndRestart(t *testing.T) {
	ctx := context.Background()
	a, s, _ := spawnApp(t, &fakeLLM{})
	s.Workdir = t.TempDir()
	a.cfg.StoreDir = t.TempDir()
	original := filepath.Join(s.Workdir, "doc.txt")
	os.WriteFile(original, []byte("before"), 0600)
	ops := a.checkpointFn(0, s)
	start, err := ops(ctx, port.CheckpointRequest{Action: "begin", Kind: "directory"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(start.Recovery) })
	fork, err := ops(ctx, port.CheckpointRequest{Action: "fork"})
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(fork.Path, "doc.txt"), []byte("candidate"), 0600)
	if _, err := ops(ctx, port.CheckpointRequest{Action: "adopt", Branch: fork.Branch}); err != nil {
		t.Fatal(err)
	}
	if _, err := ops(ctx, port.CheckpointRequest{Action: "finish"}); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(original, []byte("concurrent user edit"), 0600)
	a.finishCheckpoints(ctx, s.ID)
	data, _ := os.ReadFile(original)
	if string(data) != "concurrent user edit" {
		t.Fatal("conflicting original overwritten")
	}
	if _, err := os.Stat(fork.Path); err != nil {
		t.Fatal("recovery branch removed", err)
	}
	// Rehydrate from the durable manifest, not the old in-memory object.
	a.mu.Lock()
	a.stateLocked(s.ID).checkpoint = nil
	a.mu.Unlock()
	resumed, err := ops(ctx, port.CheckpointRequest{Action: "begin", Kind: "directory"})
	if err != nil || !resumed.Ready {
		t.Fatalf("resume: %+v %v", resumed, err)
	}
	os.WriteFile(original, []byte("before"), 0600)
	a.finishCheckpoints(ctx, s.ID)
	data, _ = os.ReadFile(original)
	if string(data) != "candidate" {
		t.Fatalf("retry did not publish: %s", data)
	}
}

func TestCheckpointRejectsLinksBusyBranchesAndForgedMetadata(t *testing.T) {
	ctx := context.Background()
	a, s, _ := spawnApp(t, &fakeLLM{})
	s.Workdir = gitRepo(t)
	w, err := a.checkpointFor(ctx, s.Workdir, string(s.ID), "git")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(w.root) })
	fork, err := w.action(ctx, port.CheckpointRequest{Action: "fork"})
	if err != nil {
		t.Fatal(err)
	}
	_, release, err := w.acquire(fork.Branch)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.action(ctx, port.CheckpointRequest{Action: "adopt", Branch: fork.Branch}); err == nil {
		t.Fatal("busy branch adopted")
	}
	release()
	if err := os.Symlink(s.Workdir, filepath.Join(fork.Path, "escape")); err == nil {
		if _, err := w.action(ctx, port.CheckpointRequest{Action: "adopt", Branch: fork.Branch}); err == nil {
			t.Fatal("link adopted")
		}
		os.Remove(filepath.Join(fork.Path, "escape"))
	}
	os.WriteFile(filepath.Join(fork.Path, ".git"), []byte("gitdir: "+filepath.Join(s.Workdir, ".git")+"\n"), 0600)
	if _, err := w.action(ctx, port.CheckpointRequest{Action: "adopt", Branch: fork.Branch}); err == nil {
		t.Fatal("forged gitdir adopted")
	}
}

func TestCheckpointCrashRecovery(t *testing.T) {
	ctx := context.Background()
	a, s, _ := spawnApp(t, &fakeLLM{})
	s.Workdir = t.TempDir()
	os.WriteFile(filepath.Join(s.Workdir, "doc"), []byte("before"), 0600)
	w, err := a.checkpointFor(ctx, s.Workdir, string(s.ID), "directory")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(w.root) })
	before := w.baseline["doc"]
	after := checkpointFile{Digest: "unknown", Mode: 0600}
	os.MkdirAll(filepath.Join(w.root, "backup"), 0700)
	os.WriteFile(filepath.Join(w.root, "backup", "doc"), []byte("before"), 0600)
	os.WriteFile(filepath.Join(s.Workdir, "doc"), []byte("after"), 0600)
	files, _ := checkpointFiles(s.Workdir)
	after = files["doc"]
	plan, _ := json.Marshal(struct {
		Paths   []string
		Desired map[string]checkpointFile
	}{[]string{"doc"}, map[string]checkpointFile{"doc": after}})
	os.WriteFile(filepath.Join(w.root, "publish-paths.json"), plan, 0600)
	w.phase = "publishing"
	w.save()
	if _, err := w.action(ctx, port.CheckpointRequest{Action: "recover"}); err != nil {
		t.Fatal(err)
	}
	restored, _ := checkpointFiles(s.Workdir)
	if restored["doc"] != before || w.ready {
		t.Fatal("crash recovery did not restore baseline")
	}
}

func TestCheckpointFinalizationThroughRealTurnCompletion(t *testing.T) {
	for _, scenario := range []string{"accepted", "cancelled", "original_conflict"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			a, _, _ := spawnApp(t, &declaringLLM{})
			a.cfg.StoreDir = t.TempDir()
			original := t.TempDir()
			os.WriteFile(filepath.Join(original, "doc"), []byte("before"), 0600)
			sid, err := a.CreateSession(ctx, command.CreateSession{Workdir: original})
			if err != nil {
				t.Fatal(err)
			}
			s := a.sessionInfo(ctx, sid)
			ops := a.checkpointFn(0, s)
			start, err := ops(ctx, port.CheckpointRequest{Action: "begin", Kind: "directory"})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.RemoveAll(start.Recovery) })
			fork, err := ops(ctx, port.CheckpointRequest{Action: "fork"})
			if err != nil {
				t.Fatal(err)
			}
			os.WriteFile(filepath.Join(fork.Path, "doc"), []byte("after"), 0600)
			if _, err := ops(ctx, port.CheckpointRequest{Action: "adopt", Branch: fork.Branch}); err != nil {
				t.Fatal(err)
			}
			if _, err := ops(ctx, port.CheckpointRequest{Action: "finish"}); err != nil {
				t.Fatal(err)
			}
			if err := a.appendPromptText(ctx, sid, event.Actor{Kind: event.ActorUser, ID: "u"}, "finish the selected local document"); err != nil {
				t.Fatal(err)
			}
			runCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			if scenario == "cancelled" {
				cancel()
			}
			if scenario == "original_conflict" {
				os.WriteFile(filepath.Join(original, "doc"), []byte("user edit"), 0600)
			}
			_, runErr := a.runLoop(runCtx, s, AgentSpec{Name: "main"}, 0, 8, true)
			if scenario != "cancelled" && runErr != nil {
				t.Fatal(runErr)
			}
			status, err := ops(ctx, port.CheckpointRequest{Action: "status"})
			if err != nil {
				t.Fatal(err)
			}
			data, _ := os.ReadFile(filepath.Join(original, "doc"))
			switch scenario {
			case "accepted":
				if status.Phase != "cleaned" || string(data) != "after" {
					t.Fatalf("accepted turn did not publish and clean: %+v %s", status, data)
				}
			case "cancelled":
				if status.Phase != "open" || string(data) != "before" {
					t.Fatalf("cancelled turn published: %+v %s", status, data)
				}
			case "original_conflict":
				if status.Phase != "open" || string(data) != "user edit" {
					t.Fatalf("conflict overwritten: %+v %s", status, data)
				}
				evs, err := a.store.Read(ctx, sid, 0)
				if err != nil {
					t.Fatal(err)
				}
				var last event.TurnFinishedData
				for _, ev := range evs {
					if ev.Type == event.TypeTurnFinished {
						if err := json.Unmarshal(ev.Data, &last); err != nil {
							t.Fatal(err)
						}
					}
				}
				if !last.Unverified || !strings.Contains(last.Reason, "checkpoint publication failed") {
					t.Fatalf("publication failure reported as complete: %+v", last)
				}
			}
		})
	}
}

func TestCheckpointTrialRefusesUnconfinedLifecycleHooks(t *testing.T) {
	a, s, _ := spawnApp(t, &fakeLLM{})
	s.Workdir = gitRepo(t)
	a.cfg.Hooks = []HookSpec{{Event: "PreToolUse", Command: "echo must-not-run"}}
	hooks := a.spawnFnFor(0, s, event.Actor{Kind: event.ActorAgent}, "trial", "experiment")
	defer hooks.Close()
	_, err := hooks.Spawn(context.Background(), port.SpawnSpec{Trial: true, Prompt: "inspect", Tools: []string{"read"}})
	if err == nil || !strings.Contains(err.Error(), "lifecycle hooks") {
		t.Fatalf("unconfined hooks admitted: %v", err)
	}
}
