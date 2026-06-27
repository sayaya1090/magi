package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// The workflow engine drives a task through a DETERMINISTIC phase pipeline. The
// app enforces the order, the per-phase toolset, and the gates; the model only
// fills each phase's content. This makes the policy robust to weak models (the
// flow is code, not a prompt the model may ignore) — the structural counterpart
// to a hand-written orchestration choreography.
//
//	localize  (read-only)         → understand + name the exact files to change
//	implement (edit)              → smallest correct change; gated on "did edit"
//	verify    (bash / real cmd)   → run build+tests; gate loops back on failure
//	review    (read-only)         → audit the diff for correctness/regressions
//	summarize                     → final report
type wfPhase struct {
	name     string
	tools    []string // structural restriction enforced by the agent allowlist
	maxSteps int      // 0 → engine default
	goal     string   // appended to the base system prompt for this phase only
}

var (
	wfReadOnly = []string{"read", "grep", "glob", "list", "findcontext"}
	wfEdit     = []string{"read", "grep", "glob", "list", "findcontext", "write", "edit", "multiedit"}
	wfVerify   = []string{"read", "grep", "glob", "list", "bash"}
)

const wfPhaseSteps = 14

func codingPhases() (localize, implement, verify, review, summarize wfPhase) {
	localize = wfPhase{name: "localize", tools: wfReadOnly, maxSteps: wfPhaseSteps,
		goal: "PHASE: LOCALIZE. Understand the task and find the EXACT file(s) and function(s) that must change. " +
			"Use findcontext/grep/glob/read — read the relevant code. Do NOT edit anything yet (you have no edit " +
			"tools this phase). Finish with a short list of the target files (path:line) and a one-line plan."}
	implement = wfPhase{name: "implement", tools: wfEdit, maxSteps: wfPhaseSteps,
		goal: "PHASE: IMPLEMENT. BEFORE editing, verify readiness: (a) Do I understand the requirement and edge cases? " +
			"(b) Have I identified all impacted files (implementation + tests + docs)? (c) Are there hidden dependencies? " +
			"If NO to any, read more files. Then make the SMALLEST correct change to the files identified above. " +
			"Edit existing files; don't touch unrelated code, don't add features or stray files. You MUST actually apply edits this phase."}
	verify = wfPhase{name: "verify", tools: wfVerify, maxSteps: wfPhaseSteps,
		goal: "PHASE: VERIFY. Build and run the tests with bash to confirm the change works and nothing regressed. " +
			"Report pass/fail with the key output. Do not edit source (no edit tools this phase)."}
	review = wfPhase{name: "review", tools: wfReadOnly, maxSteps: wfPhaseSteps,
		goal: "PHASE: REVIEW. Critique your change: (a) Does it fulfill the original requirement? " +
			"(b) Did it introduce regressions or break existing functionality? (c) Is the diff minimal, or did you touch unrelated code? " +
			"(d) Are there missed edge cases or incomplete error handling? " +
			"If you find issues, say exactly what's wrong; otherwise confirm it's correct and minimal. Do not edit."}
	summarize = wfPhase{name: "summarize", tools: []string{"read"}, maxSteps: 3,
		goal: "PHASE: SUMMARIZE. Give a brief plain-language summary of what changed and why, referencing files as " +
			"path:line, and state the verification result. Then stop."}
	return
}

// runWorkflow executes the deterministic coding pipeline on a session.
func (a *App) runWorkflow(ctx context.Context, s session.Session) error {
	localize, implement, verify, review, summarize := codingPhases()
	loops := a.cfg.WorkflowMaxLoops
	if loops <= 0 {
		loops = 3
	}

	if err := a.runPhase(ctx, s, localize, ""); err != nil {
		return err
	}

	// IMPLEMENT → VERIFY, looping until verification passes or the budget runs out.
	cmd := a.verifyCommand(s.Workdir)
	var feedback string
	verified := false
	for attempt := 1; attempt <= loops; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		// Gate 1: implement must actually change files; otherwise re-prompt.
		edited, err := a.runPhaseExpectingEdits(ctx, s, implement, feedback, attempt, loops)
		if err != nil {
			return err
		}
		if !edited {
			feedback = "No file edits were made. You must apply the change with edit/write before verifying."
			a.emitPhase(s.ID, "implement", "retry", fmt.Sprintf("no edits (attempt %d/%d)", attempt, loops))
			continue
		}

		// Gate 2: verification. A real command is authoritative; otherwise the
		// model runs tests itself (best-effort, no hard gate).
		if cmd == "" {
			_ = a.runPhase(ctx, s, verify, "")
			verified = true // no deterministic verifier available; trust the phase
			break
		}
		a.emitPhase(s.ID, "verify", "start", cmd)
		out, code := a.runVerifyCmd(ctx, s.Workdir, cmd)
		if code == 0 {
			a.emitPhase(s.ID, "verify", "pass", cmd)
			verified = true
			break
		}
		a.emitPhase(s.ID, "verify", "fail", fmt.Sprintf("exit %d (attempt %d/%d)", code, attempt, loops))
		lspDiag := a.collectLSPDiagnostics(ctx, s.Workdir)
		feedback = fmt.Sprintf("Verification command `%s` FAILED (exit %d). Fix the root cause.\n\nBuild output:\n%s",
			cmd, code, truncateOutput(out, 3000))
		if lspDiag != "" {
			feedback += "\n\n" + lspDiag
		}
	}
	if cmd != "" && !verified {
		a.injectWorkflow(ctx, s.ID, fmt.Sprintf("Verification still failing after %d attempts — in REVIEW/SUMMARIZE, "+
			"state honestly what remains broken; do not claim success.", loops))
	}

	if err := a.runPhase(ctx, s, review, ""); err != nil {
		return err
	}
	return a.runPhase(ctx, s, summarize, "")
}

// runPhase runs one phase as a constrained agent turn (restricted toolset +
// phase goal), with optional feedback injected first.
func (a *App) runPhase(ctx context.Context, s session.Session, ph wfPhase, feedback string) error {
	if feedback != "" {
		a.injectWorkflow(ctx, s.ID, feedback)
	}
	a.emitPhase(s.ID, ph.name, "start", "")
	base := a.agentFor(s)
	pa := AgentSpec{
		Name:   base.Name,
		System: base.System + "\n\n# CURRENT PHASE\n" + ph.goal,
		Tools:  ph.tools,
		Model:  base.Model,
	}
	_, err := a.runLoop(ctx, s, pa, 0, ph.maxSteps)
	a.emitPhase(s.ID, ph.name, "done", "")
	return err
}

// runPhaseExpectingEdits runs the implement phase and reports whether any file
// was actually modified during it (the implement gate).
func (a *App) runPhaseExpectingEdits(ctx context.Context, s session.Session, ph wfPhase, feedback string, attempt, loops int) (bool, error) {
	before := a.lastSeq(ctx, s.ID)
	if feedback != "" {
		a.injectWorkflow(ctx, s.ID, feedback)
	}
	a.emitPhase(s.ID, ph.name, "start", fmt.Sprintf("attempt %d/%d", attempt, loops))
	base := a.agentFor(s)
	pa := AgentSpec{Name: base.Name, System: base.System + "\n\n# CURRENT PHASE\n" + ph.goal, Tools: ph.tools, Model: base.Model}
	_, err := a.runLoop(ctx, s, pa, 0, ph.maxSteps)
	a.emitPhase(s.ID, ph.name, "done", "")
	return a.fileEditsSince(ctx, s.ID, before), err
}

// --- gates & helpers ---

// verifyCommand returns the configured verification command, or an auto-detected
// one based on the project's build system. Empty means no deterministic gate.
func (a *App) verifyCommand(workdir string) string {
	if a.cfg.VerifyCmd != "" {
		return a.cfg.VerifyCmd
	}
	return detectVerifyCmd(workdir)
}

// detectVerifyCmd guesses a build+test command from project marker files.
func detectVerifyCmd(workdir string) string {
	exists := func(rel string) bool {
		_, err := os.Stat(filepath.Join(workdir, rel))
		return err == nil
	}
	switch {
	case exists("go.mod"):
		return "go build ./... && go test ./..."
	case exists("Cargo.toml"):
		return "cargo test"
	case exists("pyproject.toml"), exists("setup.py"), exists("setup.cfg"), exists("pytest.ini"), exists("tox.ini"):
		return "python -m pytest -q"
	case exists("package.json"):
		return "npm test --silent"
	case exists("Makefile"):
		return "make test"
	}
	return ""
}

// collectLSPDiagnostics runs gopls check to collect LSP diagnostics when verification
// fails. Returns formatted diagnostics or an empty string if gopls is unavailable.
// Errors are silently ignored for graceful degradation (non-Go projects, gopls not installed, etc.).
func (a *App) collectLSPDiagnostics(ctx context.Context, workdir string) string {
	if a.plat == nil {
		return ""
	}
	// Try gopls check (Go projects only). Execution errors (gopls not found,
	// permission denied, etc.) are ignored - we fall back to build output only.
	res, err := a.plat.Exec(ctx, port.Cmd{Path: "gopls", Args: []string{"check", "./..."}, Dir: workdir})
	if err != nil || len(res.Stdout) == 0 {
		// Silently degrade: gopls not available, no diagnostics, or execution failed
		return ""
	}
	out := string(res.Stdout)
	if len(res.Stderr) > 0 {
		out += "\n" + string(res.Stderr)
	}
	if strings.TrimSpace(out) == "" {
		return ""
	}
	return fmt.Sprintf("LSP diagnostics (gopls):\n%s", truncateOutput(out, 1000))
}

// runVerifyCmd runs the verification command in the workdir, returning combined
// output and exit code. A nil platform (tests) reports "no platform".
func (a *App) runVerifyCmd(ctx context.Context, workdir, cmd string) (string, int) {
	if a.plat == nil {
		return "no platform to run verification", -1
	}
	name, args := wfShell(cmd)
	res, err := a.plat.Exec(ctx, port.Cmd{Path: name, Args: args, Dir: workdir})
	out := string(res.Stdout)
	if len(res.Stderr) > 0 {
		out += "\n" + string(res.Stderr)
	}
	if err != nil && res.ExitCode == 0 {
		return out + "\n" + err.Error(), 1
	}
	return out, res.ExitCode
}

func wfShell(cmd string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "powershell", []string{"-NoProfile", "-Command", cmd}
	}
	return "/bin/sh", []string{"-c", cmd}
}

// injectWorkflow appends a system-actor instruction so the next phase sees it.
func (a *App) injectWorkflow(ctx context.Context, sid session.SessionID, text string) {
	pd, _ := json.Marshal(event.PromptSubmittedData{
		MessageID: "m_" + newID(),
		Parts:     []session.Part{{Kind: session.PartText, Text: text}},
	})
	_ = a.appendFact(context.WithoutCancel(ctx), sid, event.TypePromptSubmitted,
		event.Actor{Kind: event.ActorSystem, ID: "workflow"}, pd)
}

// emitPhase publishes a (transient) workflow-phase event for observers.
func (a *App) emitPhase(sid session.SessionID, phase, status, detail string) {
	d, _ := json.Marshal(event.WorkflowPhaseData{Phase: phase, Status: status, Detail: detail})
	a.publishTransient(sid, event.TypeWorkflowPhase, event.Actor{Kind: event.ActorSystem, ID: "workflow"}, d)
}

// lastSeq returns the highest persisted event seq for a session (0 if none).
func (a *App) lastSeq(ctx context.Context, sid session.SessionID) int64 {
	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil || len(evs) == 0 {
		return 0
	}
	return evs[len(evs)-1].Seq
}

// fileEditsSince reports whether any successful file-modifying tool result was
// appended after fromSeq (used by the implement gate).
func (a *App) fileEditsSince(ctx context.Context, sid session.SessionID, fromSeq int64) bool {
	evs, err := a.store.Read(ctx, sid, fromSeq)
	if err != nil {
		return false
	}
	for _, e := range evs {
		if e.Seq <= fromSeq || e.Type != event.TypePartAppended {
			continue
		}
		var d event.PartAppendedData
		if json.Unmarshal(e.Data, &d) != nil {
			continue
		}
		if d.Part.Kind == session.PartToolCall && d.Part.ToolCall != nil && fileModifiers[d.Part.ToolCall.Name] {
			return true
		}
	}
	return false
}

func truncateOutput(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n…(truncated)"
}
