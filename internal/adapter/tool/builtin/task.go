package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"sync"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// Task spawns one or more subagents and returns their combined output. It is the
// orchestration primitive (D9): the model delegates work to named agents, the
// application enforces bounded recursion (D7). (F-AGENT-MULTI)
type Task struct{}

type taskArgs struct {
	Agent  string    `json:"agent"`
	Prompt string    `json:"prompt"`
	Tasks  []subTask `json:"tasks"` // optional: parallel fan-out
}

type subTask struct {
	Agent  string `json:"agent"`
	Prompt string `json:"prompt"`
}

func (Task) Name() string { return "task" }
func (Task) Description() string {
	return "Delegate work to subagents. For a single task pass {agent, prompt}. When you have SEVERAL tasks that " +
		"don't depend on each other (e.g. two reviewers looking at the same thing), pass them ALL AT ONCE as " +
		"{tasks:[{agent,prompt},...]} so they run IN PARALLEL — this is strongly preferred over dispatching them one " +
		"at a time. Only dispatch sequentially (separate calls) when a task genuinely needs an earlier task's result."
}
func (Task) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"agent":{"type":"string"},"prompt":{"type":"string"},"tasks":{"type":"array","items":{"type":"object","properties":{"agent":{"type":"string"},"prompt":{"type":"string"}}}}}}`)
}

func (Task) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	if env.Spawn == nil && env.Dispatch == nil {
		return errResult("", "subagents are not available in this context"), nil
	}
	var a taskArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errResult("", "invalid arguments: "+err.Error()), nil
	}

	reqs := a.Tasks
	if len(reqs) == 0 {
		if a.Agent == "" {
			return errResult("", "task: 'agent' (or 'tasks') is required"), nil
		}
		reqs = []subTask{{Agent: a.Agent, Prompt: a.Prompt}}
	}

	// Sidecar model: dispatch to the background and return immediately so the
	// orchestrator stays responsive. Each subagent's result is injected back as a
	// message when it completes (partial results, fault-tolerant via the
	// supervisor), and the orchestrator processes them incrementally.
	if env.Dispatch != nil {
		dispatched := make([]string, 0, len(reqs))
		var notes []string
		for _, r := range reqs {
			if r.Agent == "" {
				return errResult("", "task: each task needs an 'agent'"), nil
			}
			if note := env.Dispatch(port.SpawnRequest{Agent: r.Agent, Prompt: r.Prompt}); note != "" {
				notes = append(notes, note) // refused (e.g. already running) — surface it
			} else {
				dispatched = append(dispatched, r.Agent)
			}
		}
		var b strings.Builder
		if len(dispatched) > 0 {
			b.WriteString("Dispatched " + strings.Join(dispatched, ", ") + " in the background. " +
				"Their results will arrive as messages as each finishes — keep working on independent parts; " +
				"don't wait idly or re-dispatch them.")
		}
		for _, n := range notes {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(n)
		}
		if b.Len() == 0 {
			b.WriteString("Nothing to dispatch.")
		}
		return okText("", b.String()), nil
	}

	results := make([]port.SpawnResult, len(reqs))
	var wg sync.WaitGroup
	for i, r := range reqs {
		wg.Add(1)
		go func(i int, r subTask) {
			defer wg.Done()
			results[i] = env.Spawn(ctx, port.SpawnRequest{Agent: r.Agent, Prompt: r.Prompt})
		}(i, r)
	}
	wg.Wait()

	// Single task: return its text/error directly.
	if len(results) == 1 {
		if results[0].Err != "" {
			return errResult("", results[0].Err), nil
		}
		return okText("", results[0].Text), nil
	}

	// Multiple: label each result.
	var b strings.Builder
	anyErr := false
	for i, res := range results {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("## " + reqs[i].Agent + "\n")
		if res.Err != "" {
			anyErr = true
			b.WriteString("ERROR: " + res.Err)
		} else {
			b.WriteString(res.Text)
		}
	}
	out := okText("", b.String())
	out.IsError = anyErr
	return out, nil
}
