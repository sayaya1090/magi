// Package llm implements the Council port by polling each member over an
// LLMProvider in parallel, parsing a JSON verdict from each, and tallying them
// with the pure core/council rules. It is the I/O half of the termination gate;
// the consensus half lives in core/council and is provider-agnostic.
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// Council polls members over an LLM backend. Each member can run on its own
// backend (resolved by name) and model, so cheap and strong models can be mixed;
// a member with no provider/model falls back to the default backend and model.
type Council struct {
	resolve func(provider string) port.LLMProvider // name → backend ("" or unknown → default)
	model   string
}

// New builds an LLM-backed council. resolve maps a member's provider name to a
// backend (returning the default backend for "" or an unknown name); model is the
// fallback model when neither the member nor the request pins one.
func New(resolve func(provider string) port.LLMProvider, defaultModel string) *Council {
	return &Council{resolve: resolve, model: defaultModel}
}

// Deliberate polls every member concurrently and tallies the verdicts. A member
// that errors or returns an unparseable reply abstains (excluded from the
// denominator) rather than blocking the gate forever; if every member abstains,
// the pure tally resolves to Continue (the safe default).
func (c *Council) Deliberate(ctx context.Context, req port.DeliberationRequest) (council.Deliberation, error) {
	members := req.Members
	if len(members) == 0 {
		members = council.DefaultMembers()
	}
	rule := req.Rule
	if rule == "" {
		rule = council.DefaultRule
	}

	verdicts := make([]council.Verdict, len(members))
	var wg sync.WaitGroup
	for i, m := range members {
		wg.Add(1)
		go func(i int, m council.Member) {
			defer wg.Done()
			verdicts[i] = c.poll(ctx, req, m)
		}(i, m)
	}
	wg.Wait()

	return council.Deliberate(req.Round, verdicts, rule), nil
}

// poll asks one member and returns its verdict.
func (c *Council) poll(ctx context.Context, req port.DeliberationRequest, m council.Member) council.Verdict {
	v := council.Verdict{Member: m.Name, Lens: m.Lens, Weight: m.Weight}

	// Model: the member's pin, else the request's default (session model), else the
	// adapter's fallback. Provider: the member's named backend, else default.
	model := m.Model
	if model == "" {
		model = req.DefaultModel
	}
	if model == "" {
		model = c.model
	}
	provider := c.resolve(m.Provider)
	if provider == nil { // defensive: a resolver must yield a backend
		v.Decision = council.Abstain
		v.Rationale = "no council backend resolved for provider " + m.Provider
		return v
	}
	stream, err := provider.StreamChat(ctx, port.ChatRequest{
		Model:    model,
		System:   memberSystem(m),
		Messages: []session.Message{{Role: session.RoleUser, Parts: []session.Part{{Kind: session.PartText, Text: evidence(req)}}}},
		Params:   map[string]any{"temperature": 0.0},
	})
	if err != nil {
		v.Decision = council.Abstain
		v.Rationale = "council member unavailable: " + err.Error()
		return v
	}

	var b strings.Builder
	for ev := range stream {
		if ev.Type == port.ProviderText {
			b.WriteString(ev.Text)
		}
	}

	r, ok := parseReply(b.String())
	if !ok {
		v.Decision = council.Abstain
		v.Rationale = "unparseable council reply"
		return v
	}
	v.Decision = decisionOf(r.Decision)
	v.Confidence = r.Confidence
	v.Rationale = r.Rationale
	v.Feedback = r.Feedback
	return v
}

// memberReply is the JSON shape each member is asked to return.
type memberReply struct {
	Decision   string  `json:"decision"`
	Confidence float64 `json:"confidence"`
	Rationale  string  `json:"rationale"`
	Feedback   string  `json:"feedback"`
}

// memberSystem builds the system prompt for one member: its identity (the theme
// label) and its judging lens (the attribute), plus the strict output contract.
func memberSystem(m council.Member) string {
	lens := council.Lenses[m.Lens]
	if lens == "" {
		lens = "Judge whether the task is genuinely complete."
	}
	return fmt.Sprintf(
		"You are %s, a member of the council that decides whether an AI coding agent's turn is truly finished. "+
			"Your lens is %q: %s\n\n"+
			"Judge the agent's REPORT (its claim) against the TASK and PLAN (the contract), using the SIGNALS and DIFF "+
			"(the evidence). Do not take the agent's word for it — if a claim is not backed by evidence, that counts "+
			"against finishing. Vote \"continue\" whenever you are uncertain or evidence is missing; vote \"done\" only "+
			"when the lens is clearly satisfied.\n\n"+
			"Respond with ONLY a JSON object, no prose, no code fence:\n"+
			`{"decision":"done|continue","confidence":0.0-1.0,"rationale":"one sentence","feedback":"what remains (only if continue)"}`,
		m.Name, m.Lens, lens)
}

// evidence renders the deliberation request into the user message the members see.
func evidence(req port.DeliberationRequest) string {
	var b strings.Builder
	section := func(title, body string) {
		if strings.TrimSpace(body) == "" {
			return
		}
		b.WriteString("# " + title + "\n")
		b.WriteString(strings.TrimSpace(body))
		b.WriteString("\n\n")
	}
	section("Task (the goal)", req.Task)
	section("Plan / acceptance criteria (the contract)", req.Plan)
	section("Agent's report (the claim)", req.Report)
	if len(req.Signals) > 0 {
		b.WriteString("# Signals (the evidence)\n")
		for _, s := range req.Signals {
			line := "- "
			switch {
			case s.Source != "" && s.Kind != "":
				line += "[" + s.Source + "/" + s.Kind + "] "
			case s.Source != "":
				line += "[" + s.Source + "] "
			case s.Kind != "":
				line += "[" + s.Kind + "] "
			}
			line += s.Status
			if d := strings.TrimSpace(s.Detail); d != "" {
				line += ":\n" + d
			}
			b.WriteString(line + "\n")
		}
		b.WriteString("\n")
	}
	section("Diff", req.Diff)
	if b.Len() == 0 {
		return "No evidence was provided. Vote based on whether a task could plausibly be complete with no information (it cannot)."
	}
	return strings.TrimSpace(b.String())
}

// decisionOf maps a member's free-form decision string to a Decision. An
// unrecognized but parsed value resolves to Continue (the gate never finishes on
// an ambiguous vote).
func decisionOf(s string) council.Decision {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "done":
		return council.Done
	case "abstain":
		return council.Abstain
	default:
		return council.Continue
	}
}

// parseReply extracts the first balanced JSON object from the text (tolerating
// surrounding prose or code fences that weak models emit) and unmarshals it.
func parseReply(text string) (memberReply, bool) {
	js := firstJSONObject(text)
	if js == "" {
		return memberReply{}, false
	}
	var r memberReply
	if err := json.Unmarshal([]byte(js), &r); err != nil {
		return memberReply{}, false
	}
	if r.Decision == "" {
		return memberReply{}, false
	}
	return r, true
}

// firstJSONObject returns the first balanced {...} object in s, respecting
// strings and escapes, or "" if none.
func firstJSONObject(s string) string {
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return ""
	}
	depth := 0
	inStr := false
	esc := false
	for i := start; i < len(s); i++ {
		ch := s[i]
		switch {
		case esc:
			esc = false
		case ch == '\\' && inStr:
			esc = true
		case ch == '"':
			inStr = !inStr
		case inStr:
			// skip
		case ch == '{':
			depth++
		case ch == '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}
