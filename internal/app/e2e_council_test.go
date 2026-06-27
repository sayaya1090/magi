package app

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
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

	newSession := func(t *testing.T) (*App, session.SessionID, <-chan event.Event, func()) {
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
		gitInit(t, wd) // a real repo, so GitDiff gives the council real evidence of the work
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
		return a, sid, sub, cancelSub
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

	// Correction 1: a no-tool conversational turn must skip the council.
	t.Run("conversational turn skips council", func(t *testing.T) {
		a, sid, sub, cancelSub := newSession(t)
		defer cancelSub()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := a.Submit(ctx, command.SubmitPrompt{
			SessionID: sid,
			Parts:     []session.Part{{Kind: session.PartText, Text: "Reply with a one-sentence greeting. Do not use any tools."}},
		}); err != nil {
			t.Fatal(err)
		}
		tl := drain(t, ctx, sub)
		if tl.convened != 0 {
			t.Errorf("council convened %d times on a conversational turn, want 0", tl.convened)
		}
		t.Logf("conversational turn finished with council convened=%d (want 0)", tl.convened)
	})

	// Correction 2: a real coding turn convenes the council and terminates.
	t.Run("coding turn convenes and terminates", func(t *testing.T) {
		a, sid, sub, cancelSub := newSession(t)
		defer cancelSub()
		// Fetch the workdir from the session to verify the file later.
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
		defer cancel()
		if err := a.Submit(ctx, command.SubmitPrompt{
			SessionID: sid,
			Parts:     []session.Part{{Kind: session.PartText, Text: "Create a file named hello.txt containing exactly: magi works"}},
		}); err != nil {
			t.Fatal(err)
		}
		tl := drain(t, ctx, sub)

		if tl.convened == 0 {
			t.Error("council should convene on a turn that used tools, but it did not")
		}
		if len(tl.decided) == 0 {
			t.Fatal("council convened but never decided")
		}
		last := tl.decided[len(tl.decided)-1]
		t.Logf("council convened=%d, final decision=%q note=%q", tl.convened, last.Decision, last.Note)
		// The fix must produce GENUINE convergence, not a cap-forced finish: on a
		// clearly-complete task with no signals, the verification lens abstains
		// (nothing to verify) instead of reflexively voting continue, so the
		// remaining members reach "done". A "unresolved after N rounds" note means
		// the council still looped to the cap — the bug is not fixed.
		if strings.Contains(last.Note, "unresolved after") {
			t.Errorf("council hit the round cap instead of converging: note=%q (members still reflexively voting continue)", last.Note)
		}
	})
}
