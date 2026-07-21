package builtin

import (
	"bytes"
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
	Tasks  flexTasks `json:"tasks"` // optional: parallel fan-out
}

type subTask struct {
	Agent  string `json:"agent"`
	Prompt string `json:"prompt"`
}

// flexTasks is the `tasks` fan-out array with the same tolerance rationale as
// flexInt/flexBool: the whole delegation call was being rejected over the field's
// SHAPE, not its content. Observed live (fix-ocaml-gc bench): a model stuck in a
// search loop tried to escape by fanning out, but emitted tasks as a JSON *string*
// ("[{...}]") — a double-encoded array — and got "cannot unmarshal string into
// []subTask". The call died, the model stayed stuck, and the run failed on the
// stall guard. The escape hatch must not hinge on the model getting array-vs-string
// exactly right. Accepted shapes: a real array [{…}]; a double-encoded string
// "[{…}]" (unwrap once and retry); a single object {agent,prompt} (wrap it). Junk
// falls back to empty, which the caller treats as "no fan-out" and reads Agent/Prompt.
type flexTasks []subTask

func (t *flexTasks) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	// Proper array — the normal path.
	if b[0] == '[' {
		var arr []subTask
		if err := json.Unmarshal(b, &arr); err != nil {
			return err
		}
		*t = arr
		return nil
	}
	// Double-encoded array: the value is a JSON string whose content is itself a
	// JSON array (or object). Unwrap the string once and re-parse.
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		s = strings.TrimSpace(s)
		if s == "" {
			return nil
		}
		return t.UnmarshalJSON([]byte(s))
	}
	// Single object instead of a one-element array — wrap it.
	if b[0] == '{' {
		var one subTask
		if err := json.Unmarshal(b, &one); err != nil {
			return err
		}
		*t = []subTask{one}
		return nil
	}
	return nil // unparseable shape → no fan-out (caller falls back to agent/prompt)
}

func (Task) Name() string { return "task" }
func (Task) Description() string {
	return "Delegate work to subagents. The agent name MUST be one listed under 'Available agents' in your " +
		"system prompt — those are the ONLY agents that exist. Do NOT invent role names (analyst, researcher, " +
		"report-writer, …): a made-up name fails the call and wastes the step; if no listed agent fits, do the " +
		"work yourself. For a single task pass {agent, prompt} — e.g. {agent:\"explore\", prompt:\"find where X is configured\"}. " +
		"When you have SEVERAL tasks that don't depend on each other (e.g. two reviewers looking at the same thing), " +
		"pass them ALL AT ONCE as {tasks:[{agent,prompt},...]} so they run IN PARALLEL — e.g. " +
		"{tasks:[{agent:\"explore\",prompt:\"map the parser\"},{agent:\"explore\",prompt:\"map the emitter\"}]}. `tasks` is " +
		"a real JSON array, not a string. This is strongly preferred over dispatching one at a time. Only dispatch " +
		"sequentially (separate calls) when a task genuinely needs an earlier task's result."
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
