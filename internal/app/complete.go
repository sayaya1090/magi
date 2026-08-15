package app

import (
	"context"
	"strings"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// completeCap bounds the context sent on a completion. A person can open a 40,000-line file in the
// console and the buffer travels on every pause in typing; an unbounded prompt here is somebody's
// context window and their bill. Smaller than lookOverCap on purpose — a completion needs the code
// AROUND the cursor, not the whole tree, and the tighter the prompt the faster the answer.
const completeCap = 24 << 10

// complete runs ONE thin model call and returns its text: no session, no event log, no council, no
// tools. It is the shared engine under the IDE helpers (CompleteCode, SuggestPrompt).
//
// Why it is not a turn — the same reason as LookOver (git.go), only more so. These fire on a pause
// in typing, many times a minute, and a keystroke cannot wait seconds for a finish vote or leave a
// discarded draft in the conversation. So the whole turn machinery is off this path; what is left
// is one StreamChat to a routed profile.
//
// The ctx is the caller's, and cancellation is the design: the next keystroke cancels the ctx of the
// completion in flight, which drops the StreamChat and this returns whatever partial text drained
// (the caller discards it). profile names an [llm.profiles.*] backend via providerFor; an unset or
// unregistered profile falls through to the default provider, so callers that must NOT spend the
// main model on keystrokes check the profile is set BEFORE calling (see CompleteCode).
func (a *App) complete(ctx context.Context, profile, model, system, user string) (string, error) {
	if strings.TrimSpace(user) == "" {
		return "", nil
	}
	req := port.ChatRequest{
		Model:  model,
		System: system,
		Messages: []session.Message{{
			Role:  session.RoleUser,
			Parts: []session.Part{{Kind: session.PartText, Text: user}},
		}},
	}
	stream, err := a.providerFor(AgentSpec{Provider: profile}).StreamChat(ctx, req)
	if err != nil {
		return "", err
	}
	out, _ := drainStream(stream)
	return out, nil
}

// openFile is the console editor's current unsaved buffer for a session.
type openFile struct {
	path string
	text string
}

// SetOpenFile records the file a session's console editor has open, so the agent's next turn sees
// the unsaved buffer as ambient context (volatileContext injects it when [autocomplete] ambient is
// on). An empty path or text clears it — the editor closed the file, and stale drafts must not keep
// riding into the context after the person moved on. Never persisted.
func (a *App) SetOpenFile(sid session.SessionID, path, text string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if strings.TrimSpace(path) == "" || strings.TrimSpace(text) == "" {
		delete(a.openFiles, sid)
		return
	}
	if a.openFiles == nil {
		a.openFiles = map[session.SessionID]openFile{}
	}
	a.openFiles[sid] = openFile{path: path, text: text}
}

// openFileFor returns the session's open editor buffer, if any.
func (a *App) openFileFor(sid session.SessionID) (openFile, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	f, ok := a.openFiles[sid]
	return f, ok
}

// completeModel resolves the model name a profile runs: the profile's own model (ProfileModels,
// filled from [llm.profiles.*]) when known, else the fallback the caller passes (the session's
// model). The provider is chosen by profile name in providerFor; the model still travels in the
// request body, so a profile with no registered model would otherwise send an empty model string.
func (a *App) completeModel(profile, fallback string) string {
	if m := a.cfg.ProfileModels[profile]; m != "" {
		return m
	}
	return fallback
}

// CompleteCode returns inline completion text to insert at the cursor in a file the user is editing
// in the console. prefix is the buffer up to the cursor and suffix the buffer after it, so the model
// sees both sides (fill-in-the-middle) rather than only what came before.
//
// It self-disables when code completion is off or no code profile is routed, returning "" — never
// falling back to the main model, because that would bill the strong model on every keystroke. The
// answer is raw insertion text: any code fence or echo of the surrounding lines the model adds anyway
// is trimmed, since only the insertion is usable.
func (a *App) CompleteCode(ctx context.Context, sid session.SessionID, path, prefix, suffix string) (string, error) {
	profile := a.cfg.Autocomplete.CodeProfile
	if !a.cfg.Autocomplete.CodeOn() || profile == "" {
		return "", nil
	}
	if strings.TrimSpace(prefix)+strings.TrimSpace(suffix) == "" {
		return "", nil
	}
	// Keep the code NEAR the cursor when the buffer is large: the tail of the prefix and the head of
	// the suffix are what a completion is about, and a 40k-line file would otherwise blow the cap on
	// text the model does not need.
	if len(prefix) > completeCap {
		prefix = "… (earlier lines omitted)\n" + prefix[len(prefix)-completeCap:]
	}
	if len(suffix) > completeCap {
		suffix = suffix[:completeCap] + "\n… (later lines omitted)"
	}
	s := a.sessionInfo(ctx, sid)
	system := "You are an inline code completion engine. Continue the code exactly at the point marked " +
		"<CURSOR>, using the code after the cursor to stay consistent. Output ONLY the raw text to " +
		"insert at the cursor: no explanation, no code fences, and do not repeat the surrounding lines. " +
		"If nothing sensible completes here, output nothing at all."
	user := "File: " + path + "\n\n" + prefix + "<CURSOR>" + suffix
	out, err := a.complete(ctx, profile, a.completeModel(profile, s.Model.Model), system, user)
	if err != nil {
		return "", err
	}
	return strings.Trim(out, "`\n"), nil
}
