package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/lang"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// ---- helpers ----

// toolSpecs returns the tools available to an agent, honoring its allowlist.
func (a *App) toolSpecs(agent AgentSpec) []port.ToolSpec {
	var specs []port.ToolSpec
	for _, t := range a.tools.List() {
		name := t.Name()
		if !agent.allows(name) {
			continue
		}
		// An INTERNAL tool is advertised only to an agent whose allowlist names it. allows() reads
		// an empty list as "everything", which is what the main agent has — so this is what keeps a
		// plugin's narrow helper off every request the main agent makes while still reaching the
		// child that was spawned with it.
		meta := port.ToolMetaOf(t)
		if meta.Internal && len(agent.Tools) == 0 {
			continue
		}
		// A subagent the user switched off is not offered to the model at all. Advertising is the
		// gate: there is no second place a disabled one could still be reached from.
		if meta.Subagent && a.subagentDisabled(name, meta) {
			continue
		}
		switch name {
		case "ask_user":
			// Only an interactive session has a human to ask; headless has no one to block on.
			if !a.cfg.Interactive {
				continue
			}
		}
		specs = append(specs, port.ToolSpec{Name: name, Description: t.Description(), Schema: t.Schema()})
	}
	return specs
}

// systemFor builds the system prompt for an agent: durable project memory (AGENTS.md) + the
// agent's own prompt + what the runtime environment is.
func (a *App) systemFor(agent AgentSpec, workdir string) string {
	sys := agent.System
	if mem := a.projectMemory(workdir); mem != "" {
		sys = "# Project memory\n" + mem + "\n\n" + sys
	}
	// Tell the agent its runtime environment so it picks correct shell commands (GNU vs
	// BSD flags, package manager, path style) instead of guessing.
	sys += "\n\n" + envInfo(workdir)
	// Static, so it doesn't perturb the prefix (KV) cache across steps. Applies to every
	// agent: markdown tables render/align well everywhere and never hurt in raw text.
	sys += outputFormatGuide
	return sys
}

// volatileContext builds the per-step changing context — the current plan (TODOs), shared
// experience, and plugin-retrieved (RAG) context — that previously lived in the system
// prompt. It is now injected as an ephemeral trailing message instead, so the system prompt
// stays byte-stable within a turn and the backend's prefix (KV) cache survives across steps.
// Returns "" when there is nothing to inject. Gating matches the old in-`sys` behavior:
// experience applies to subagents too, RAG is top-level only.
// raw is reconstruct(evs) computed by the caller — the step loop already needs it for
// compaction sizing, and reconstruct is O(events), so it is built once per step and
// shared rather than re-derived here for the retrieval query.
func (a *App) volatileContext(ctx context.Context, s session.Session, agent AgentSpec, evs []event.Event, raw []session.Message, step, maxSteps int, elapsed time.Duration) string {
	var b strings.Builder
	// There is no step ceiling any more, so there is no budget to report. What is left is the one
	// number that was always magi's own and always true: how long this has been running. A model
	// that has been grinding for twenty minutes can decide for itself that the approach is wrong —
	// which is what the ceiling was trying to force, minus the forcing. No external information is
	// in it: it is magi's own stopwatch, started when the turn started.
	if elapsed >= time.Minute {
		b.WriteString(fmt.Sprintf("\n\nYou have been working for %s of wall-clock time so far.", fmtElapsed(elapsed)))
	}
	// User-set time budget (--time-budget, default off): a legitimate user input, stated as
	// guidance. Kept OFF for leaderboard/official comparison runs.
	if tb := a.cfg.TimeBudget; tb > 0 {
		rem := tb - elapsed
		if rem < 0 {
			b.WriteString(fmt.Sprintf(" The user asked for this to finish within %s — that budget is now EXCEEDED: land the smallest honest result immediately.", fmtElapsed(tb)))
		} else {
			b.WriteString(fmt.Sprintf(" The user asked for this to finish within %s (about %s remaining).", fmtElapsed(tb), fmtElapsed(rem)))
		}
	}
	if td := a.Todos(s.ID); len(td) > 0 {
		b.WriteString("\n\n# Current plan (TODOs)\n" + formatTodos(td))
	}
	// The state of the run, re-rendered every step from magi's own record — the calls it granted,
	// how they really ended, the paths they wrote, and the background commands still alive. A
	// screen-driven agent re-reads its terminal before every decision, so its view of the world is
	// never older than the last thing it did; magi's data was all recorded and none of it was ever
	// handed back, so the agent's picture of what it had done was whatever it remembered saying.
	// This is the same refresh, over the store magi actually keeps.
	if st := a.runState(evs); st != "" {
		b.WriteString("\n\n" + st)
	}
	// Compacted-context RAG (push half): topics an earlier compaction shed that look
	// lexically relevant to the current task, as one-line pointers into recall_context.
	//
	// Only for an agent that may actually CALL it. A pointer at a tool the allowlist refuses is
	// not merely wasted context: it is an instruction the agent cannot carry out, and a model
	// told to call something reads that as available and calls it — then gets a refusal, and
	// repeats, because nothing in its window says the tool is gone. See gateAllowlist.
	if agent.allows("recall_context") {
		b.WriteString(shardHints(evs, currentTaskText(evs)))
	}
	// Both retrieval hooks below key on the last user prompt, which is constant across a
	// turn; the per-turn caches absorb the (identical) lookups the remaining steps repeat.
	// The task, not whatever magi last said — a nudge landing here made every retrieval for the
	// rest of the turn a lookup about "you've run many steps without changing anything". raw is
	// reconstruct(evs), so the two agree in practice; the fallback keeps a caller that hands over
	// messages without their events from losing retrieval altogether.
	retrievalQ := taskSeedText(evs)
	if retrievalQ == "" {
		retrievalQ = lastUserText(raw)
	}
	// Shared experience (D13): advertise only how many team memories/skills match the
	// current request — a one-line pointer, not the entries themselves. The agent pulls
	// the detail on demand with recall_memory, so relevant knowledge stays reachable
	// without spending context on it every turn — which is only true for an agent whose
	// allowlist HAS recall_memory. A restricted agent (the read-only spec-mine explorer, a
	// workflow phase, a delegate with a narrowed set) was being handed the pointer anyway and
	// then refused when it followed it.
	if a.cfg.Experience != nil && retrievalQ != "" && agent.allows("recall_memory") {
		if p := a.experiencePointerCached(ctx, s.ID, retrievalQ, agent.Groups); p != "" {
			b.WriteString("\n\n# Shared experience\n" + p)
		}
	}
	// Plugin-registered context providers (RAG).
	if q := retrievalQ; q != "" {
		if c := a.gatherContextCached(ctx, s, q); c != "" {
			b.WriteString("\n\n# Retrieved context\n" + c)
		}
	}
	// The file the person has open in the console editor, unsaved. On by default ([autocomplete]
	// ambient) so a question about "this file" is answered against the buffer they are looking at
	// rather than stale disk. Gated on the read allowlist like the pointers above — an agent that may
	// not read files has no business being handed one — and ephemeral: it rides the same trailing-
	// message path as the rest of volatileContext and is never persisted.
	if a.cfg.Autocomplete.AmbientOn() && agent.allows("read") {
		if f, ok := a.openFileFor(s.ID); ok {
			txt := f.text
			if len(txt) > completeCap {
				txt = txt[:completeCap] + "\n… (the rest of this file is not shown)"
			}
			b.WriteString("\n\n# File open in the editor (unsaved)\nThe user has " + f.path +
				" open and is editing it; this is their current buffer, which may differ from disk:\n" + txt)
		}
	}
	return strings.TrimSpace(b.String())
}

// experiencePointerCached memoizes the shared-experience pointer per (session, query).
// Retrieve re-reads and re-scores every memory/skill file on each call, and the query —
// the last user prompt — is constant across a turn, so without the cache every loop step
// repeats an identical scan. Cached on success only: a transient Retrieve error keeps the
// per-step retry the uncached code had. Invalidated by a successful Propose (see the tool
// env in executeTool) so a memory the agent just saved is advertised immediately.
func (a *App) experiencePointerCached(ctx context.Context, sid session.SessionID, q string, groups []string) string {
	a.mu.Lock()
	st := a.stateLocked(sid)
	key := q + "\x00" + strings.Join(groups, ",")
	if st.expPtrQ == key {
		p := st.expPtr
		a.mu.Unlock()
		return p
	}
	a.mu.Unlock()
	mems, skills, err := a.cfg.Experience.Retrieve(ctx, q, groups)
	if err != nil {
		return ""
	}
	p := experiencePointer(len(mems), len(skills))
	a.mu.Lock()
	st = a.stateLocked(sid)
	st.expPtrQ, st.expPtr = key, p
	a.mu.Unlock()
	return p
}

// gatherContextCached memoizes the plugin-RAG context block per (session, query). Unlike
// the experience pointer, an EMPTY result is cached too: gatherContext folds provider
// errors into "" (best-effort), and a provider that errors or times out (5s each) would
// otherwise re-block every remaining step of the turn on the same query — exactly the
// per-step stall this cache exists to remove. Invalidated by RegisterContextProvider.
func (a *App) gatherContextCached(ctx context.Context, s session.Session, q string) string {
	a.mu.Lock()
	st := a.stateLocked(s.ID)
	if st.ragQ == q {
		c := st.ragText
		a.mu.Unlock()
		return c
	}
	a.mu.Unlock()
	c := a.gatherContext(ctx, port.ContextQuery{SessionID: s.ID, Workdir: s.Workdir, Prompt: q})
	a.mu.Lock()
	st = a.stateLocked(s.ID)
	st.ragQ, st.ragText = q, c
	a.mu.Unlock()
	return c
}

// envInfo describes the runtime environment (OS, arch, shell, workdir, date) so the model
// chooses commands that actually work on this host rather than assuming Linux/GNU. The
// reported shell MUST match what the bash tool actually invokes (see builtin.shell): on
// Windows that is PowerShell, not sh — reporting "sh" there made models emit Linux/GNU
// syntax that fails.
func envInfo(workdir string) string {
	return buildEnvInfo(runtime.GOOS, runtime.GOARCH, os.Getenv("SHELL"), workdir, time.Now().Format("2006-01-02"), osReleaseOnce())
}

// osReleaseOnce reads /etc/os-release at most once per process — the file is immutable
// for the host's lifetime, and systemFor (hence envInfo) runs every loop step, so a
// per-step read would be pure waste. Non-Linux hosts skip the read entirely.
var osReleaseOnce = sync.OnceValue(func() string {
	if runtime.GOOS != "linux" {
		return ""
	}
	// Distro identity drives which package manager the model should use; without it,
	// models guess (apt on Alpine, etc.). /etc/os-release is the freedesktop standard.
	b, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}
	return string(b)
})

// buildEnvInfo is the pure core of envInfo (goos/os-release injectable for testing).
// shellEnv is $SHELL (ignored on Windows, where the bash tool always uses PowerShell);
// osRelease is the raw /etc/os-release contents (Linux only, "" when unavailable).
func buildEnvInfo(goos, arch, shellEnv, workdir, date, osRelease string) string {
	shell := "sh"
	extra := ""
	switch goos {
	case "windows":
		shell = "powershell"
		extra = "\n- Note: the `bash` tool runs commands through PowerShell (`powershell -NoProfile -Command`). " +
			"Use PowerShell/Windows syntax (e.g. `Get-ChildItem`, `Remove-Item`, `$env:VAR`, `;` to chain), " +
			"NOT Linux/GNU commands (`ls`, `rm`, `grep`, `&&`) or POSIX paths."
	case "linux":
		if shellEnv != "" {
			shell = filepath.Base(shellEnv)
		}
		extra = linuxDistroLines(osRelease)
	case "darwin":
		if shellEnv != "" {
			shell = filepath.Base(shellEnv)
		}
		// macOS ships no system package manager; Homebrew is the de-facto one. Steer the
		// model to it instead of guessing apt/yum, and to the BSD userland (not GNU).
		extra = "\n- Package manager: brew (install with `brew install <pkg>`) — macOS has no apt/yum" +
			"\n- Note: BSD userland — some flags differ from GNU (e.g. `sed -i ''`, no `sed -i` bare)."
	default:
		if shellEnv != "" {
			shell = filepath.Base(shellEnv)
		}
	}
	return fmt.Sprintf("# Environment\n- OS: %s (%s)\n- Shell: %s\n- Working directory: %s\n- Date: %s%s",
		goos, arch, shell, workdir, date, extra)
}

// linuxDistroLines renders the distro name/version and its package manager from raw
// /etc/os-release contents so the model uses the right install command. Empty when the
// file was unavailable or unparseable.
func linuxDistroLines(osRelease string) string {
	if osRelease == "" {
		return ""
	}
	kv := parseOSRelease(osRelease)
	name := kv["PRETTY_NAME"]
	if name == "" {
		name = strings.TrimSpace(kv["NAME"] + " " + kv["VERSION_ID"])
	}
	if name == "" {
		return ""
	}
	out := "\n- Distro: " + name
	if pm := linuxPackageManager(kv["ID"], kv["ID_LIKE"]); pm != "" {
		out += fmt.Sprintf("\n- Package manager: %s (install with `%s`) — prefer it; don't assume apt/yum", pm, pmInstallHint(pm))
	}
	return out
}

// parseOSRelease parses KEY=VALUE lines (values optionally quoted) from os-release.
func parseOSRelease(content string) map[string]string {
	m := map[string]string{}
	for _, ln := range strings.Split(content, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "#") {
			continue
		}
		k, v, ok := strings.Cut(ln, "=")
		if !ok {
			continue
		}
		m[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
	}
	return m
}

// linuxPackageManager maps an os-release ID / ID_LIKE to its package manager. ID_LIKE
// lets derivatives (Mint→ubuntu→debian, Rocky→rhel) resolve without an explicit entry.
func linuxPackageManager(id, idLike string) string {
	for _, f := range strings.Fields(strings.ToLower(id + " " + idLike)) {
		switch f {
		case "debian", "ubuntu":
			return "apt"
		case "fedora", "rhel", "centos", "rocky", "almalinux":
			return "dnf"
		case "alpine":
			return "apk"
		case "arch", "manjaro":
			return "pacman"
		case "opensuse", "suse", "sles":
			return "zypper"
		}
	}
	return ""
}

func pmInstallHint(pm string) string {
	switch pm {
	case "apt":
		return "apt-get install -y <pkg>"
	case "dnf":
		return "dnf install -y <pkg>"
	case "apk":
		return "apk add <pkg>"
	case "pacman":
		return "pacman -S --noconfirm <pkg>"
	case "zypper":
		return "zypper install -y <pkg>"
	}
	return pm + " install <pkg>"
}

// outputFormatGuide steers tabular output toward markdown tables. Replies render as
// markdown (glamour), which lays out `| a | b |` tables and measures DISPLAY width — so
// a CJK/wide cell (2 cells) still aligns. A hand-padded ASCII/box table instead counts
// runes, so any wide character shifts that column out of line; passing width numbers to
// the model can't fix this (it streams tokens before their width is known). The cure is
// to not hand-align at all: emit a markdown table and let the renderer align it.
const outputFormatGuide = "\n\n# Output formatting\n" +
	"Your replies render as GitHub-flavored markdown in a terminal. To present tabular data, use a markdown " +
	"table (| col | col | with a |---|---| separator row) — the renderer aligns the columns for you, correctly " +
	"accounting for wide/CJK characters that take two cells. Do NOT hand-align columns with spaces or draw " +
	"ASCII/box-drawing tables: space padding counts characters, not display width, so any CJK or other wide " +
	"content shifts the columns out of alignment."

// langDirective inspects the user's latest message and, when it's written in a
// non-Latin script, returns a short forceful instruction (placed first in the
// system prompt) to answer in that language. Weak local models otherwise drift
// back to English regardless of a buried "match the user's language" rule.
func langDirective(text string) string { return lang.Directive(text) }

func oneLineHint(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.Join(strings.Fields(s), " ")
}
