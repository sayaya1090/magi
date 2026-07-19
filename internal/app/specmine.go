package app

import (
	"context"
	"strings"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// elicitSpecMineSystem instructs the signature-mining elicitation: read ONLY what the
// request itself exposes (names, parameter/return types, formats) and expand it into
// the requirements those surfaces imply plus the standard construct for the job. The
// output is consumed verbatim by the executor as a finished note, so it must be short,
// concrete, and free of speculation beyond what the surfaces actually state.
const elicitSpecMineSystem = "You extract the requirements a coding request encodes in its NAMES and TYPE " +
	"SIGNATURES rather than its prose. From the request's identifiers, parameter/return types, and stated " +
	"formats, list as short bullets: (1) constraints they imply that the prose leaves unsaid — a type " +
	"constrains what its values (and their lifecycles) can be or do; a name like max_*/timeout_*/n_* states " +
	"an exact bound; a format fixes shape; (2) for the situation those surfaces describe, the STANDARD " +
	"construct/idiom the language or its stdlib provides for exactly that job (name it), in preference to " +
	"hand-assembling the mechanism from lower-level parts — the idiom already carries the edge semantics " +
	"(ordering, cancellation, partial failure) a hand-rolled version drops. Derive ONLY what the given " +
	"surfaces actually imply — do not invent requirements. ADDITIONS ONLY: never restate what the " +
	"request's prose already says explicitly — the reader has the request; repeating it dilutes the " +
	"note. If the names and types imply nothing beyond the prose, output exactly NONE. Otherwise " +
	"output the bullet list only, no preamble."

// elicitSpecMine asks the model (tool-free) to mine the request's identifiers and type
// signatures for implied requirements and the standard idiom. Empty string on failure —
// the caller treats it as best-effort. Uses the agent's provider (per-agent routing).
func (a *App) elicitSpecMine(ctx context.Context, agent AgentSpec, s session.Session, task string) string {
	req := port.ChatRequest{
		Model:    s.Model.Model,
		System:   elicitSpecMineSystem,
		Messages: []session.Message{{Role: session.RoleUser, Parts: []session.Part{{Kind: session.PartText, Text: task}}}},
	}
	stream, err := a.providerFor(agent).StreamChat(ctx, req)
	if err != nil {
		return ""
	}
	var b strings.Builder
	for ev := range stream {
		if ev.Type == port.ProviderText {
			b.WriteString(ev.Text)
		}
	}
	out := strings.TrimSpace(b.String())
	// The elicitation answers NONE when the surfaces add nothing beyond the prose —
	// treat that (and trivial echoes of it) as "inject nothing".
	if len(out) < 8 && strings.Contains(strings.ToUpper(out), "NONE") {
		return ""
	}
	return out
}

// specMineNote wraps a mined result for injection into the main session. The header
// mirrors the other execution notes so the executor reads it as system guidance.
func specMineNote(mined string) string {
	return "# Execution note — requirements mined from the request's identifiers/types\n" +
		"Derived from the request's own names and type signatures (not from its prose). Honor these " +
		"alongside the stated requirements, and prefer the named standard construct over hand-rolling:\n" +
		mined
}
