package app

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	councilllm "github.com/sayaya1090/magi/internal/adapter/council/llm"
	"github.com/sayaya1090/magi/internal/adapter/llm/openai"
	"github.com/sayaya1090/magi/internal/adapter/platform"
	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// TestE2ECouncilGate exercises the council (on-by-default) against a real model
// to verify the two corrections to its default-on behaviour:
//
//  1. A purely conversational turn (a greeting, no tools) must NOT convene the
//     council — the user is not held in a deliberation loop over small talk.
//  2. A real coding turn (the agent writes a file) MUST convene the council and
//     terminate within bounded rounds — the reworded member prompts vote "done"
//     once the task is satisfied instead of reflexively voting "continue".
//
// Configure via MAGI_E2E_OLLAMA_BASE (default localhost) + _MODEL; skipped when
// no backend is reachable so the suite stays green offline.
func TestE2ECouncilGate(t *testing.T) {
	base := os.Getenv("MAGI_E2E_OLLAMA_BASE")
	if base == "" {
		base = "http://localhost:11434/v1"
	}
	if base == "disabled" || !reachable(base) {
		t.Skipf("ollama not reachable at %s", base)
	}
	model := os.Getenv("MAGI_E2E_OLLAMA_MODEL")
	if model == "" {
		model = "qwen3-coder:30b"
	}

	llm := openai.New(base, os.Getenv("MAGI_E2E_API_KEY"))
	// The council runs on the same backend/model as the agent.
	council := councilllm.New(func(string) port.LLMProvider { return llm }, model)

	gitInit := func(t *testing.T, dir string) {
		t.Helper()
		for _, args := range [][]string{
			{"init", "-q"},
			{"config", "user.email", "e2e@magi.test"},
			{"config", "user.name", "magi e2e"},
			{"commit", "--allow-empty", "-q", "-m", "init"},
		} {
			cmd := exec.Command("git", args...)
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v\n%s", args, err, out)
			}
		}
	}

	// seed writes files into a fresh git repo and commits them, so a turn that
	// MODIFIES or READS them has tracked history (and the council a real base).
	seed := func(t *testing.T, dir string, files map[string]string) {
		t.Helper()
		for name, body := range files {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		for _, args := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", "seed"}} {
			cmd := exec.Command("git", args...)
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v\n%s", args, err, out)
			}
		}
	}

	newSession := func(t *testing.T, seedFiles map[string]string) (*App, session.SessionID, string, <-chan event.Event, func()) {
		t.Helper()
		store, err := jsonl.New(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		a := New(store, llm, builtin.Default(), bus.New(), platform.New(), Config{
			Model:            session.ModelRef{Provider: "openai", Model: model},
			System:           "You are a coding agent in a working directory. To create files you MUST call the write tool with {path, content}.",
			Permission:       "allow",
			MaxSteps:         40, // high enough that the council round-cap, not the step-cap, governs
			Council:          council,
			CouncilMaxRounds: 3, // bound the gate so a misbehaving model can't hang the test
		})
		wd := t.TempDir()
		gitInit(t, wd) // a real repo (harmless; the council now judges tool-reconstructed changes, not git)
		if len(seedFiles) > 0 {
			seed(t, wd, seedFiles)
		}
		sid, err := a.CreateSession(context.Background(), command.CreateSession{
			Workdir: wd, Model: session.ModelRef{Provider: "openai", Model: model},
		})
		if err != nil {
			t.Fatal(err)
		}
		sub, cancelSub, err := a.Subscribe(context.Background(), sid, 0)
		if err != nil {
			t.Fatal(err)
		}
		return a, sid, wd, sub, cancelSub
	}

	// drain consumes events until turn.finished, returning the council tally.
	type tally struct {
		convened int
		decided  []event.CouncilDecidedData
	}
	drain := func(t *testing.T, ctx context.Context, sub <-chan event.Event) tally {
		t.Helper()
		var tl tally
		for {
			select {
			case e, ok := <-sub:
				if !ok {
					t.Fatal("stream closed before turn finished")
				}
				switch e.Type {
				case event.TypeCouncilConvened:
					tl.convened++
				case event.TypeCouncilVerdict:
					var v event.CouncilVerdictData
					_ = json.Unmarshal(e.Data, &v)
					t.Logf("  r%d %-10s [%s] %-8s %s", v.Round, v.Member, v.Lens, v.Decision, v.Rationale)
				case event.TypeCouncilDecided:
					var d event.CouncilDecidedData
					_ = json.Unmarshal(e.Data, &d)
					tl.decided = append(tl.decided, d)
					t.Logf("  r%d DECIDED %s %s", d.Round, d.Decision, d.Note)
				case event.TypeError:
					t.Fatalf("loop error: %s", string(e.Data))
				case event.TypeTurnFinished:
					return tl
				}
			case <-ctx.Done():
				t.Fatal("timeout waiting for turn to finish")
			}
		}
	}

	// run submits a prompt and drains the turn.
	run := func(t *testing.T, seedFiles map[string]string, prompt string) tally {
		t.Helper()
		a, sid, _, sub, cancelSub := newSession(t, seedFiles)
		defer cancelSub()
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
		defer cancel()
		if err := a.Submit(ctx, command.SubmitPrompt{
			SessionID: sid,
			Parts:     []session.Part{{Kind: session.PartText, Text: prompt}},
		}); err != nil {
			t.Fatal(err)
		}
		return drain(t, ctx, sub)
	}

	// assertConverges asserts a working turn convened the council and reached a
	// GENUINE "done" — not a cap-forced finish. A "unresolved after N rounds" note
	// means members still reflexively voted continue (the bug is not fixed). This
	// is the over-fitting guard: it must hold for EVERY working-turn shape below,
	// not just "create a new file".
	assertConverges := func(t *testing.T, tl tally) {
		t.Helper()
		if tl.convened == 0 {
			t.Error("council should convene on a turn that used tools, but it did not")
		}
		if len(tl.decided) == 0 {
			t.Fatal("council convened but never decided")
		}
		last := tl.decided[len(tl.decided)-1]
		t.Logf("council convened=%d, final decision=%q note=%q", tl.convened, last.Decision, last.Note)
		if strings.Contains(last.Note, "unresolved after") {
			t.Errorf("council hit the round cap instead of converging: note=%q", last.Note)
		}
	}

	// Correction 1: a no-tool conversational turn must skip the council.
	t.Run("conversational turn skips council", func(t *testing.T) {
		tl := run(t, nil, "Reply with a one-sentence greeting. Do not use any tools.")
		if tl.convened != 0 {
			t.Errorf("council convened %d times on a conversational turn, want 0", tl.convened)
		}
		t.Logf("conversational turn finished with council convened=%d (want 0)", tl.convened)
	})

	// Correction 2 — new file: the diff carries the new file's content (the fix).
	t.Run("new file converges", func(t *testing.T) {
		assertConverges(t, run(t, nil, "Create a file named hello.txt containing exactly: magi works"))
	})

	// Over-fitting guard 1 — modifying a TRACKED file: `git diff` shows the edit
	// directly (this path always worked); the council must still converge on it.
	t.Run("tracked modification converges", func(t *testing.T) {
		tl := run(t,
			map[string]string{"greeting.txt": "hello there\n"},
			"Edit greeting.txt so its entire contents are exactly: magi rules")
		assertConverges(t, tl)
	})

	// Over-fitting guard 2 — a READ-ONLY turn: it uses a tool (so the gate fires)
	// but produces NO diff. There is no diff evidence to lean on, so this is the
	// true test that members ABSTAIN on their evidence-less lens (verification)
	// and judge the rest against the report — instead of churning "continue" for
	// lack of a diff. If the diff fix were over-fitted, this would loop to the cap.
	t.Run("read-only turn converges", func(t *testing.T) {
		tl := run(t,
			map[string]string{"config.txt": "host=localhost\nport=8080\ndebug=false\n"},
			"Read config.txt and tell me which port is configured. Do not modify any files.")
		assertConverges(t, tl)
	})

	// Regression for the heavy read-only churn: a turn that reads several files and
	// makes broad claims (no diff, no signals) must still converge, not loop to the
	// cap. This is the case that previously hung — the NoChanges signal + the
	// "absence of a diff is not a defect" prompt must carry it to a genuine finish.
	t.Run("heavy read-only converges", func(t *testing.T) {
		tl := run(t,
			map[string]string{
				"a.md": "# Module A\nHandles authentication and sessions.\n",
				"b.md": "# Module B\nHandles storage and persistence.\n",
				"c.md": "# Module C\nHandles the HTTP API surface.\n",
			},
			"Read every .md file in this directory and give a one-line summary of each. Do not modify anything.")
		assertConverges(t, tl)
	})
}
