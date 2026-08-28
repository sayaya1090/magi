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
// WindowOf is contextWindow for the wiring layer: the authoritative window for a model, after the
// registry, the probe and the [limits] context_tokens override have had their say.
//
// Exported for one caller — the LLM client, which holds a configured output cap UNDER it. The cap
// used to go out as a demand however little room was left (measured: a 393,216 window, 93,217 in,
// a cap of 300,000, and the turn died one token over), and the two numbers could not meet because
// the window is resolved here and the cap is applied at the wire.
func (a *App) WindowOf(id string) int { return a.contextWindow(id) }

// VisionOf says whether a model reads pictures, from the same table WindowOf answers from.
//
// Exported for the same one caller, and for the same reason: the table is filled in while magi runs
// — a plugin contributes models, and the window probe registers what it learns — so an adapter that
// kept its own copy of the built-in catalogue would answer for a model nobody had declared yet.
//
// A model the table does not know — and whose family it does not know either — answers false. The
// table answers a ":tag" variant from its family, which is how "qwen3-vl:8b" inherits the facts of
// "qwen3-vl" and how every other field in it behaves; a name with no relative in the table gets
// nothing.
//
// False is the safe direction: an image block sent to a backend that cannot read one is an error,
// or silently dropped content the model is then asked about, while a model that CAN see and is told
// it cannot still gets the line of text naming the file.
func (a *App) VisionOf(id string) bool { return a.cfg.Models.Get(id).Vision }

// windowMark is why a model will not be probed again — three reasons that shared one slot until a
// redirect needed to tell them apart. Only the pinned one outlives a change of backend.
type windowMark uint8

const (
	windowProbing windowMark = iota // a probe is in flight, or ran and found nothing
	windowProbed                    // a backend answered, and that entry is in the registry
	windowPinned                    // a person set it with /context
)

func (a *App) contextWindow(id string) int {
	if id == "" {
		return 0
	}
	if a.cfg.ContextTokens > 0 {
		return a.cfg.ContextTokens // [limits] context_tokens: the operator's number, for every model
	}
	a.forgetWindowsOfAnotherBackend()
	if a.cfg.Models.Has(id) {
		return a.cfg.Models.Get(id).ContextWindow
	}
	a.mu.Lock()
	_, seen := a.probingWindows[id]
	if !seen && a.cfg.ContextWindowProber != nil {
		a.probingWindows[id] = windowProbing // mark before unlocking so we probe at most once
		a.mu.Unlock()
		// Fall back to the family window while the exact-window probe runs, so a variant
		// we already have a sane window for (e.g. "qwen3-coder:480b-cloud" inheriting
		// "qwen3-coder") isn't reported as 0/unlimited — which would disable ratio
		// compaction at exactly the session-start budget calc — until the probe lands.
		// Read it BEFORE launching the goroutine: the family window is a static seed the
		// probe never writes, so this stays deterministic and free of the probe-write race
		// (Get on an id the probe DID populate would read a non-zero window nondeterministically).
		fallback := a.cfg.Models.Get(id).ContextWindow
		go a.probeContextWindow(id)
		return fallback
	}
	a.mu.Unlock()
	return a.cfg.Models.Get(id).ContextWindow
}

// probeContextWindow asks the backend for id's real window and registers it, so
// subsequent contextWindow calls (Has == true) return the accurate value. A
// failed probe leaves id marked in probingWindows so we don't hammer the backend
// (until requests are pointed at a different one, which the mark does not outlive).
// It uses RegisterIfAbsent, not Register: an in-flight probe must NOT clobber a
// manual /context override that SetContextWindow set while the probe was running
// (marking probingWindows only blocks a FUTURE probe, not this one) — so if the id
// already has a value by the time the probe lands, the probe defers to it.
func (a *App) probeContextWindow(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	w, ok := a.cfg.ContextWindowProber(ctx, id)
	if !ok || w <= 0 {
		return
	}
	// The registration and the mark that records where it came from, under one lock. Apart, a
	// /context pin could land between them — taking the entry, then being recorded as something a
	// backend gave us, so the next redirect would throw away a number a person typed. Held
	// together the pair has no in-between to land in: SetContextWindow either writes its entry
	// first, and RegisterIfAbsent defers to it, or it writes after both of these and overwrites
	// them. Neither ordering leaves the entry saying one thing and the mark another.
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cfg.Models.RegisterIfAbsent(model.Info{ID: id, ContextWindow: w, MaxOutput: w / 4, Tools: true}) {
		a.probingWindows[id] = windowProbed
	}
}

// forgetWindowsOfAnotherBackend drops what we learned from a backend we are no longer talking to.
//
// A window is a fact about a (backend, model) pair, not about a model: two servers can serve the
// same name — probe.go's own reason for existing is that one started with a 96K num_ctx answers
// differently from one that did not — and this cache was keyed by the name alone. So after a
// /route switch, or a plugin pointing the agent at its own gateway, compaction went on sizing
// itself against the OLD server's number. Too large is the dangerous direction: the turn is not
// compacted and the backend refuses it, which is the exact failure the probe was written to
// prevent.
//
// Checked here, on the read, rather than announced by whoever redirects: the console's switch, a
// plugin's set_base_url and a hot reload all reach the same client and none of them tells the App.
// A redirect nobody remembered to report is the shape this tree keeps finding.
//
// A pinned window stays. A person who typed /context was not talking about a backend.
func (a *App) forgetWindowsOfAnotherBackend() {
	reader, ok := a.llm.(interface{ BaseURL() string })
	if !ok {
		return // a provider that cannot say where it points cannot have moved
	}
	base := reader.BaseURL()
	a.mu.Lock()
	if base == a.windowBase {
		a.mu.Unlock()
		return
	}
	a.windowBase = base
	var forget []string
	for id, mark := range a.probingWindows {
		if mark == windowPinned {
			continue
		}
		// windowProbing too: a backend that could not answer said nothing about the next one, and
		// the mark that stopped us hammering IT must not stop us asking THIS one.
		forget = append(forget, id)
		delete(a.probingWindows, id)
	}
	a.mu.Unlock()
	for _, id := range forget {
		a.cfg.Models.Forget(id)
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
	a.probingWindows[id] = windowPinned // keep the manual value: don't lazy-probe over it, and don't
	// drop it when the backend changes — this number is the person's, not a backend's
	a.mu.Unlock()
	if tokens == 0 {
		return fmt.Sprintf("context window for %s set to unlimited", id), nil
	}
	return fmt.Sprintf("context window for %s set to %s tokens", id, commas(tokens)), nil
}
