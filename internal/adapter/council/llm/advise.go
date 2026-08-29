package llm

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// adviseSystem is the whole instruction. It is short on purpose.
//
// The council's own doctrine is ~20 KB — three lenses, their routes, the evidence rules, the
// requirements-walk format, the JSON schema — and every byte of it is about judging a TURN. A
// caller asking one question about one command needs none of it, and sending it was not merely
// wasteful: it told the reader to answer as verdicts, so a reader that answered the question
// instead looked like a parse failure.
const adviseSystem = "You are one careful reader, answering one question for an agent runtime.\n\n" +
	"Answer in prose, in a few sentences. Lead with the answer itself — the first word should be " +
	"yes or no when the question admits one — and then the reason. Do not write JSON, do not write " +
	"a verdict, do not score anything. Judge only what is asked; if the question is about whether " +
	"something is required, the task's own wording is what settles it."

// Advise answers one question in prose, from one reader. See port.Council.
func (c *Council) Advise(ctx context.Context, req port.AdviceRequest) (string, error) {
	members := req.Members
	if len(members) == 0 {
		members = council.DefaultMembers()
	}
	if len(members) == 0 {
		return "", fmt.Errorf("no council member to ask")
	}
	m := members[0]
	model := m.Model
	if model == "" {
		model = req.DefaultModel
	}
	if model == "" {
		model = c.model
	}
	provider := c.resolve(m.Provider)
	if provider == nil {
		return "", fmt.Errorf("no council backend resolved for provider %q", m.Provider)
	}
	user := strings.TrimSpace(req.Question)
	if t := strings.TrimSpace(req.Task); t != "" {
		user = "── THE TASK ──\n" + t + "\n\n── THE QUESTION ──\n" + user
	}
	stream, err := provider.StreamChat(ctx, port.ChatRequest{
		Model:  model,
		System: withLangNote(adviseSystem, req.Task),
		Messages: []session.Message{{Role: session.RoleUser,
			Parts: []session.Part{{Kind: session.PartText, Text: user}}}},
		Params: map[string]any{"temperature": 0.0},
	})
	if err != nil {
		return "", err
	}
	text, cut := drain(stream)
	if cut != nil && strings.TrimSpace(text) == "" {
		return "", cut
	}
	if cut != nil {
		// The caller decides on this prose, and the prompt asks for yes/no FIRST — so a reply cut
		// mid-qualification reads as an unqualified yes. Every other reader in this package logs
		// its cut; this one returned the fragment as if it were whole.
		fmt.Fprintf(os.Stderr, "magi: an advisory answer was cut off after %d chars: %v\n", len(text), cut)
	}
	return strings.TrimSpace(text), nil
}
