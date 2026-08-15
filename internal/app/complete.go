package app

import (
	"context"
	"strings"
	"unicode/utf8"

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
// completion in flight, which drops the StreamChat. A cut or cancelled stream is returned as an error
// with NO text — a truncated completion is worse than none, since it would be inserted mid-token, so
// the callers treat any error as "no completion" and insert nothing. profile names an [llm.profiles.*]
// backend via providerFor; callers that must NOT spend the main model on keystrokes verify the profile
// is registered BEFORE calling (see completeReady / CompleteCode), because providerFor falls through
// to the default provider for an unregistered name.
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
	out, derr := drainStream(stream)
	if derr != nil {
		return "", derr // a cut/cancelled stream: no partial text is ever inserted
	}
	return out, nil
}

// completeReady reports whether a completion surface is usable and, if so, the model to send. A
// surface is usable only when its profile is BOTH registered as a provider (else providerFor would
// fall through to the default — the main model — and bill it on every keystroke) AND resolves to a
// model to send in the request body. Either missing, the surface self-disables rather than spending
// or misrouting. profile "" (unset) is never ready.
func (a *App) completeReady(profile string) (model string, ok bool) {
	if profile == "" {
		return "", false
	}
	a.mu.Lock()
	registered := a.providers[profile] != nil
	a.mu.Unlock()
	if !registered {
		return "", false
	}
	m := a.cfg.ProfileModels[profile]
	if m == "" {
		return "", false // a routed backend with no model: we cannot know what to ask it for
	}
	return m, true
}

// clampHeadUTF8 / clampTailUTF8 keep at most max BYTES of s while never splitting a multibyte rune,
// so what reaches the model is always valid UTF-8. Head keeps the front, tail keeps the end.
func clampHeadUTF8(s string, max int) string {
	if len(s) <= max {
		return s
	}
	s = s[:max]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

func clampTailUTF8(s string, max int) string {
	if len(s) <= max {
		return s
	}
	s = s[len(s)-max:]
	for len(s) > 0 && !utf8.RuneStart(s[0]) {
		s = s[1:]
	}
	return s
}

// stripCodeFence removes a whole ```lang … ``` wrapper a model adds despite being told not to,
// WITHOUT touching backticks that are part of the code (a template literal, a raw string, inline
// markdown). It only acts when the text is a single fenced block: opening fence on the first line,
// closing fence at the end. Anything else is returned unchanged.
func stripCodeFence(s string) string {
	t := strings.TrimSpace(s)
	if !strings.HasPrefix(t, "```") || !strings.HasSuffix(t, "```") || len(t) < 6 {
		return s
	}
	nl := strings.IndexByte(t, '\n')
	if nl < 0 {
		return s // one line that both opens and closes a fence — not a wrapped block
	}
	inner := t[nl+1:]
	inner = strings.TrimSuffix(inner, "```")
	return strings.TrimRight(inner, "\n")
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
	// Stored clamped, not just clamped on read: volatileContext shows at most completeCap of it, so
	// holding the whole of a 40MB buffer per session for the daemon's life is memory for nothing.
	a.openFiles[sid] = openFile{path: path, text: clampHeadUTF8(text, completeCap)}
}

// openFileFor returns the session's open editor buffer, if any.
func (a *App) openFileFor(sid session.SessionID) (openFile, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	f, ok := a.openFiles[sid]
	return f, ok
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
	if !a.cfg.Autocomplete.CodeOn() {
		return "", nil
	}
	model, ok := a.completeReady(a.cfg.Autocomplete.CodeProfile)
	if !ok {
		return "", nil // no fast code profile routed — never fall through to the main model
	}
	if strings.TrimSpace(prefix)+strings.TrimSpace(suffix) == "" {
		return "", nil
	}
	// Keep the code NEAR the cursor when the buffer is large: the tail of the prefix and the head of
	// the suffix are what a completion is about, and a 40k-line file would otherwise blow the cap on
	// text the model does not need. Clamped on a rune boundary so a CJK/emoji file never sends a split
	// character.
	if len(prefix) > completeCap {
		prefix = "… (earlier lines omitted)\n" + clampTailUTF8(prefix, completeCap)
	}
	if len(suffix) > completeCap {
		suffix = clampHeadUTF8(suffix, completeCap) + "\n… (later lines omitted)"
	}
	system := "You are an inline code completion engine. Continue the code exactly at the point marked " +
		"<CURSOR>, using the code after the cursor to stay consistent. Output ONLY the raw text to " +
		"insert at the cursor: no explanation, no code fences, and do not repeat the surrounding lines. " +
		"If nothing sensible completes here, output nothing at all."
	user := "File: " + path + "\n\n" + prefix + "<CURSOR>" + suffix
	out, err := a.complete(ctx, a.cfg.Autocomplete.CodeProfile, model, system, user)
	if err != nil {
		return "", nil // a cut/cancelled/failed completion inserts nothing
	}
	return stripCodeFence(out), nil
}
