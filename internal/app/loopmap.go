package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// LoopMap returns a compact structural map of the session's loop(s) — turns,
// steps, tool activity, and council rounds — projected from the event log. It
// makes the loop's SHAPE visible (loop-engineering pain #2: non-linear/ephemeral
// observability) instead of a flat transcript scroll.
func (a *App) LoopMap(ctx context.Context, sid session.SessionID) (string, error) {
	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		return "", err
	}
	return buildLoopMap(evs), nil
}

// loopTurn accumulates one user turn's shape.
type loopTurn struct {
	prompt  string
	steps   int      // model invocations (distinct assistant messages)
	tools   int      // tool calls issued
	errs    int      // tool results that were errors
	council []string // per-round summaries
	planned bool     // a plan-stage event occurred
	usage   *event.Usage
}

// buildLoopMap is the pure projection (events → map text), so it is unit-testable
// without the store.
func buildLoopMap(evs []event.Event) string {
	var turns []*loopTurn
	seenMsg := map[string]bool{}
	cur := func() *loopTurn {
		if len(turns) == 0 {
			turns = append(turns, &loopTurn{prompt: "(no prompt)"})
		}
		return turns[len(turns)-1]
	}

	for _, e := range evs {
		switch e.Type {
		case event.TypePromptSubmitted:
			// A real user prompt starts a new turn; system injections (hooks,
			// council feedback, auto-orchestrate) belong to the current turn.
			if e.Actor.Kind != event.ActorUser {
				if e.Stage == stagePlan {
					cur().planned = true
				}
				continue
			}
			var d event.PromptSubmittedData
			_ = json.Unmarshal(e.Data, &d)
			turns = append(turns, &loopTurn{prompt: firstLine(partsText(d.Parts), 60)})

		case event.TypePartAppended:
			var d event.PartAppendedData
			if json.Unmarshal(e.Data, &d) != nil {
				continue
			}
			t := cur()
			if e.Stage == stagePlan {
				t.planned = true
			}
			switch {
			case d.Role == session.RoleAssistant && (d.Part.Kind == session.PartText || d.Part.Kind == session.PartToolCall):
				if !seenMsg[d.MessageID] {
					t.steps++
					seenMsg[d.MessageID] = true
				}
				if d.Part.Kind == session.PartToolCall {
					t.tools++
				}
			case d.Role == session.RoleTool && d.Part.Kind == session.PartToolResult:
				if d.Part.ToolResult != nil && d.Part.ToolResult.IsError {
					t.errs++
				}
			}

		case event.TypeCouncilDecided:
			var d event.CouncilDecidedData
			if json.Unmarshal(e.Data, &d) != nil {
				continue
			}
			s := fmt.Sprintf("r%d %d✓/%d→ %s", d.Round, d.Tally.Done, d.Tally.Continue, d.Decision)
			if d.Note != "" {
				s += " (" + d.Note + ")"
			}
			t := cur()
			t.council = append(t.council, s)

		case event.TypeTurnFinished:
			var d event.TurnFinishedData
			if json.Unmarshal(e.Data, &d) == nil {
				u := d.Usage
				cur().usage = &u
			}
		}
	}

	if len(turns) == 0 {
		return "Loop map — no turns yet."
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Loop map — %d turn(s)\n", len(turns))
	for i, t := range turns {
		fmt.Fprintf(&b, "\nTurn %d: %s\n", i+1, t.prompt)
		if t.planned {
			b.WriteString("  ◈ plan\n")
		}
		line := fmt.Sprintf("  %s · %s", plural(t.steps, "step"), plural(t.tools, "tool call"))
		if t.errs > 0 {
			line += fmt.Sprintf(" · %s", plural(t.errs, "error"))
		}
		b.WriteString(line + "\n")
		if len(t.council) > 0 {
			b.WriteString("  ⚖ council: " + strings.Join(t.council, " · ") + "\n")
		}
		if t.usage != nil {
			fmt.Fprintf(&b, "  ✓ finalize · %d in / %d out\n", t.usage.In, t.usage.Out)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// partsText concatenates the text of message parts.
func partsText(parts []session.Part) string {
	var b strings.Builder
	for _, p := range parts {
		if p.Kind == session.PartText {
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

// firstLine returns the first line of s, trimmed and truncated to n runes.
func firstLine(s string, n int) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	if s == "" {
		return "(empty)"
	}
	return s
}

// plural renders "1 step" / "3 steps".
func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}
