// Package eval is a small quantitative harness: it runs a fixed task suite
// through the real magi app layer against any OpenAI-compatible backend and
// reports success rate, steps, tool calls, subagent spawns, tokens, and wall
// time. The harness is held constant so results compare MODELS (e.g. a local
// qwen vs Gemini vs Claude), turning "performance" from a feature checklist into
// numbers. See eval_test.go for the runner entry point.
package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// Task is one scored scenario.
type Task struct {
	Name    string
	Prompt  string
	Seed    map[string]string // workdir-relative files to create before the run
	Timeout time.Duration     // 0 → default
	// Check decides success from the final reply, the workdir, and the metrics.
	Check func(workdir, reply string, r Result) (bool, string)
}

// Result is the measured outcome of one task.
type Result struct {
	Task                string
	Finished, Success   bool
	AsstMsgs, ToolCalls int
	Spawns              int
	TokIn, TokOut       int
	Dur                 time.Duration
	Note                string
}

// orchestratorSystem is a representative top-level prompt (the app layer adds the
// language lock, subagent contract, and guardrails automatically). Held constant
// across models so differences reflect the model, not the prompt.
const orchestratorSystem = "You are magi, a terminal coding agent. Use tools (read/write/edit/grep/glob/list/bash) " +
	"to do the user's task in the working directory; never ask the user to paste files. For work that splits into " +
	"independent pieces, delegate to subagents with the task tool — dispatch independent pieces together as " +
	"tasks:[...] so they run in parallel — then synthesize their results once they arrive and finish. Keep replies concise."

func evalAgents() map[string]app.AgentSpec {
	ro := []string{"read", "grep", "glob", "list", "ask", "report"}
	return map[string]app.AgentSpec{
		"coder":    {Name: "coder", System: "Review or implement from a coding perspective. Be concise.", Tools: append(append([]string{}, ro...), "write", "edit", "multiedit", "bash")},
		"tester":   {Name: "tester", System: "Review from a testing/verification perspective. Be concise.", Tools: append(append([]string{}, ro...), "bash")},
		"reviewer": {Name: "reviewer", System: "Review the given files and report concrete issues. Be concise.", Tools: ro},
	}
}

// Run executes the suite against one backend and returns per-task results.
func Run(llm port.LLMProvider, model string, plat port.Platform, tasks []Task) ([]Result, error) {
	out := make([]Result, 0, len(tasks))
	for _, task := range tasks {
		r, err := runTask(llm, model, plat, task)
		if err != nil {
			return out, err
		}
		out = append(out, r)
	}
	return out, nil
}

func runTask(llm port.LLMProvider, model string, plat port.Platform, task Task) (Result, error) {
	r := Result{Task: task.Name}
	dir, err := os.MkdirTemp("", "magi-eval-")
	if err != nil {
		return r, err
	}
	defer os.RemoveAll(dir)
	for rel, content := range task.Seed {
		p := filepath.Join(dir, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			return r, err
		}
	}
	store, err := jsonl.New(filepath.Join(dir, ".store"))
	if err != nil {
		return r, err
	}
	reg := builtin.Default()
	reg.Register(builtin.Task{})
	reg.Register(builtin.Ask{})
	reg.Register(builtin.Report{})
	ref := session.ModelRef{Provider: "openai", Model: model}
	a := app.New(store, llm, reg, bus.New(), plat, app.Config{
		Model:      ref,
		System:     orchestratorSystem,
		Permission: "allow",
		MaxSteps:   30,
		Agents:     evalAgents(),
	})

	timeout := task.Timeout
	if timeout == 0 {
		timeout = 4 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	sid, err := a.CreateSession(ctx, command.CreateSession{Workdir: dir, Model: ref})
	if err != nil {
		return r, err
	}
	sub, cancelSub, err := a.Subscribe(ctx, sid, 0)
	if err != nil {
		return r, err
	}
	defer cancelSub()

	a.Submit(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: task.Prompt}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "eval"},
	})

	start := time.Now()
	var reply strings.Builder
	for done := false; !done; {
		select {
		case e, ok := <-sub:
			if !ok {
				done = true
				break
			}
			switch e.Type {
			case event.TypeAgentSpawned:
				r.Spawns++
			case event.TypePartAppended:
				var d event.PartAppendedData
				if json.Unmarshal(e.Data, &d) == nil && d.Role == session.RoleAssistant {
					switch d.Part.Kind {
					case session.PartText:
						r.AsstMsgs++
						reply.WriteString(d.Part.Text)
						reply.WriteString("\n")
					case session.PartToolCall:
						r.ToolCalls++
					}
				}
			case event.TypeTurnFinished:
				var d event.TurnFinishedData
				if json.Unmarshal(e.Data, &d) == nil {
					r.TokIn += d.Usage.In
					r.TokOut += d.Usage.Out
				}
				r.Finished = true
				done = true
			case event.TypeError:
				var d event.ErrorData
				_ = json.Unmarshal(e.Data, &d)
				r.Finished = true
				r.Note = "error: " + d.Code
				done = true
			}
		case <-ctx.Done():
			r.Note = "timeout"
			done = true
		}
	}
	r.Dur = time.Since(start)
	if task.Check != nil {
		ok, note := task.Check(dir, reply.String(), r)
		r.Success = ok
		if note != "" {
			r.Note = note
		}
	}
	return r, nil
}

// --- success helpers ---

func fileEquals(dir, rel, want string) bool {
	b, err := os.ReadFile(filepath.Join(dir, rel))
	return err == nil && strings.TrimSpace(string(b)) == strings.TrimSpace(want)
}

func fileContains(dir, rel, want string) bool {
	b, err := os.ReadFile(filepath.Join(dir, rel))
	return err == nil && strings.Contains(string(b), want)
}

func isKorean(s string) bool {
	n := 0
	for _, r := range s {
		if (r >= 0xAC00 && r <= 0xD7A3) || (r >= 0x1100 && r <= 0x11FF) {
			n++
		}
	}
	return n >= 2
}

// DefaultSuite is the fixed scored task set. It deliberately mixes single-agent
// mechanics with the multi-agent orchestration that is magi's fragile spot.
func DefaultSuite() []Task {
	design := "# magi Design\n\nHexagonal core, event-sourced JSONL store, OpenAI-compatible LLM port. Subagents via a task tool.\n"
	return []Task{
		{
			Name:    "read-comprehend",
			Seed:    map[string]string{"notes.md": "The build command for this repo is: go build ./...\n"},
			Prompt:  "What is the build command for this repo? Answer in one short line.",
			Timeout: 90 * time.Second,
			Check: func(_, reply string, _ Result) (bool, string) {
				return strings.Contains(reply, "go build"), ""
			},
		},
		{
			Name:    "write-file",
			Prompt:  "Create a file named out.txt whose entire contents are exactly: pong",
			Timeout: 90 * time.Second,
			Check: func(dir, _ string, _ Result) (bool, string) {
				return fileEquals(dir, "out.txt", "pong"), ""
			},
		},
		{
			Name:    "edit-file",
			Seed:    map[string]string{"conf.txt": "mode=off\nlevel=1\n"},
			Prompt:  "In conf.txt change mode=off to mode=on. Change nothing else.",
			Timeout: 90 * time.Second,
			Check: func(dir, _ string, _ Result) (bool, string) {
				return fileContains(dir, "conf.txt", "mode=on") && fileContains(dir, "conf.txt", "level=1"), ""
			},
		},
		{
			Name:    "locate-symbol",
			Seed:    map[string]string{"a/util.go": "package a\n\nfunc Helper() {}\n", "b/main.go": "package b\n\nfunc Run() {}\n"},
			Prompt:  "Which file defines the function Helper? Reply with just the file path.",
			Timeout: 90 * time.Second,
			Check: func(_, reply string, _ Result) (bool, string) {
				return strings.Contains(reply, "util.go"), ""
			},
		},
		{
			Name:    "language-korean",
			Prompt:  "이 도구가 무엇인지 한 문장으로 설명해 줘.",
			Timeout: 90 * time.Second,
			Check: func(_, reply string, _ Result) (bool, string) {
				if isKorean(reply) {
					return true, ""
				}
				return false, "replied non-Korean"
			},
		},
		{
			Name:    "delegate-synthesize",
			Seed:    map[string]string{"DESIGN.md": design},
			Prompt:  "Have the coder and tester subagents review DESIGN.md, then give me one combined synthesis.",
			Timeout: 5 * time.Minute,
			Check: func(_, reply string, r Result) (bool, string) {
				if !r.Finished {
					return false, "did not finish"
				}
				if r.Spawns < 1 {
					return false, "never delegated"
				}
				if strings.TrimSpace(reply) == "" {
					return false, "empty synthesis"
				}
				note := ""
				if r.Spawns == 2 {
					note = "parallel-2"
				} else if r.Spawns > 2 {
					note = fmt.Sprintf("re-dispatch x%d", r.Spawns)
				}
				return true, note
			},
		},
	}
}

// Report renders a comparison table plus a summary line.
func Report(model string, rs []Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n=== eval: %s ===\n", model)
	fmt.Fprintf(&b, "%-22s %-5s %-5s %5s %5s %6s %6s %8s  %s\n", "task", "fin", "ok", "asst", "tool", "spawn", "tok-in", "dur", "note")
	pass, totIn, totOut := 0, 0, 0
	var totDur time.Duration
	for _, r := range rs {
		if r.Success {
			pass++
		}
		totIn += r.TokIn
		totOut += r.TokOut
		totDur += r.Dur
		fmt.Fprintf(&b, "%-22s %-5v %-5v %5d %5d %6d %6d %7.0fs  %s\n",
			r.Task, r.Finished, r.Success, r.AsstMsgs, r.ToolCalls, r.Spawns, r.TokIn, r.Dur.Seconds(), r.Note)
	}
	rate := 0.0
	if len(rs) > 0 {
		rate = 100 * float64(pass) / float64(len(rs))
	}
	fmt.Fprintf(&b, "--- %d/%d passed (%.0f%%)  tokens in/out=%d/%d  total=%s ---\n",
		pass, len(rs), rate, totIn, totOut, totDur.Round(time.Second))
	return b.String()
}

// SortByName keeps task order stable across runs for easy diffing.
func SortByName(rs []Result) {
	sort.Slice(rs, func(i, j int) bool { return rs[i].Task < rs[j].Task })
}
