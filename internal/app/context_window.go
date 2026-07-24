package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/core/model"
	"github.com/sayaya1090/magi/internal/core/session"
)

// isContextOverflow reports whether a provider error is a rejection for exceeding the model's
// context window (as opposed to a transient/network failure). OpenAI returns code
// "context_length_exceeded"; vLLM/OpenAI-compatible backends phrase it "This model's maximum
// context length is N tokens, however you requested M"; Anthropic-via-LiteLLM says "prompt is
// too long". The match is on the message text (case-insensitive) since the wire error is a
// plain 400 whose body varies by backend. Kept deliberately narrow — an unrelated 400 must not
// trigger a compaction-and-retry.
func isContextOverflow(err error) bool {
	if err == nil {
		return false
	}
	m := strings.ToLower(err.Error())
	for _, sig := range []string{
		"context_length_exceeded",
		"maximum context length",
		"context length",
		"reduce the length",
		"reduce the number of tokens",
		"prompt is too long",
		"too many tokens",
		"exceeds the context",
		"exceed context",
		"input is too long",
	} {
		if strings.Contains(m, sig) {
			return true
		}
	}
	return false
}

// ModelWindow is one model currently in use and its resolved context window
// (0 = unlimited/unknown). Session marks the top-level session model.
type ModelWindow struct {
	Model   string
	Window  int
	Session bool
}

// ContextWindows lists the distinct models in use for a session — the session
// model plus every per-agent route and defined profile — each with its resolved
// context window, so the UI can show and edit them individually. (Different
// agents can run different models, so their windows differ.)
func (a *App) ContextWindows(ctx context.Context, sid session.SessionID) []ModelWindow {
	s := a.sessionInfo(ctx, sid)
	seen := map[string]bool{}
	var out []ModelWindow
	add := func(id string, sess bool) {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, ModelWindow{Model: id, Window: a.contextWindow(id), Session: sess})
	}
	add(s.Model.Model, true) // session model first, flagged
	for _, r := range a.AgentRoutes(sid) {
		add(r.Model, false)
	}
	for _, p := range a.Profiles() {
		add(p.Model, false)
	}
	return out
}

// contextWindow returns the model's context window in tokens, resolving it in
// this order: a seeded/registered/user-set entry wins; otherwise, the first time
// an unseeded model is seen, it kicks off a one-shot background probe of the LLM
// backend (ContextWindowProber) and registers the result for next time. Until
// that probe lands — and for models with no usable window — it returns 0, which
// every consumer treats as "unlimited / unknown" (no % gauge, no ratio
// compaction). The probe runs in a goroutine so this hot-path call never blocks.
func (a *App) contextWindow(id string) int {
	if id == "" {
		return 0
	}
	if a.cfg.Models.Has(id) {
		return a.cfg.Models.Get(id).ContextWindow
	}
	a.mu.Lock()
	_, seen := a.probingWindows[id]
	if !seen && a.cfg.ContextWindowProber != nil {
		a.probingWindows[id] = struct{}{} // mark before unlocking so we probe at most once
		a.mu.Unlock()
		go a.probeContextWindow(id)
		// This first read must report 0 (unlimited) while the probe runs — return it
		// directly rather than re-reading the registry, which the just-launched goroutine
		// could already have populated (a race that reads as a non-zero first window).
		return 0
	}
	a.mu.Unlock()
	return a.cfg.Models.Get(id).ContextWindow
}

// probeContextWindow asks the backend for id's real window and registers it, so
// subsequent contextWindow calls (Has == true) return the accurate value. A
// failed probe leaves id marked in probingWindows so we don't hammer the backend.
func (a *App) probeContextWindow(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	if w, ok := a.cfg.ContextWindowProber(ctx, id); ok && w > 0 {
		a.cfg.Models.Register(model.Info{ID: id, ContextWindow: w, MaxOutput: w / 4, Tools: true})
	}
}

// SetContextWindow manually overrides the context window (tokens) for a model,
// e.g. from the /context slash command. An empty model targets the session's
// current model. tokens <= 0 sets it to unlimited (0). It preserves the model's
// other metadata and blocks a later lazy probe from clobbering the override.
// Returns a human-readable note.
func (a *App) SetContextWindow(ctx context.Context, sid session.SessionID, id string, tokens int) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		id = a.sessionInfo(ctx, sid).Model.Model
	}
	if id == "" {
		return "", fmt.Errorf("session has no model")
	}
	if tokens < 0 {
		tokens = 0
	}
	info := a.cfg.Models.Get(id)
	info.ID = id
	info.ContextWindow = tokens
	a.cfg.Models.Register(info)
	a.mu.Lock()
	a.probingWindows[id] = struct{}{} // keep the manual value: don't lazy-probe over it
	a.mu.Unlock()
	if tokens == 0 {
		return fmt.Sprintf("context window for %s set to unlimited", id), nil
	}
	return fmt.Sprintf("context window for %s set to %s tokens", id, commas(tokens)), nil
}
