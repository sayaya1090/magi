package app

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// Pure event/message extraction and formatting helpers used by the run loop: locating the
// most recent genuine user prompt (vs. injected feedback), enumerating user prompts across a
// turn, and rendering the experience/todo blocks. No App state; split out of loop.go for
// cohesion. Behavior unchanged.

// lastUserPromptText returns the text of the most recent GENUINE user prompt
// (Actor.Kind == user), skipping council/hook/auto injections (which are recorded
// as user-role prompts but authored by the system). Used for the language lock so
// injected English feedback can't unlock the user's language.
func lastUserPromptText(evs []event.Event) string {
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Type == event.TypePromptSubmitted && evs[i].Actor.Kind == event.ActorUser {
			var d event.PromptSubmittedData
			if json.Unmarshal(evs[i].Data, &d) == nil {
				return partsText(d.Parts)
			}
		}
	}
	return ""
}

// userPrompt is a genuine user prompt with the id of the event that carried it, so the
// interjection detector can mask that exact event while the message stays queued.
type userPrompt struct {
	MsgID string
	Text  string
}

// seedPromptIdx returns the index (in userPromptEntries order) of the genuine user
// prompt that SEEDS the current top-level turn: the first user prompt not already
// answered by an assistant reply. It is meaningful only at step 0, where the current
// turn has produced no output yet — so any assistant (ActorAgent) part in the log
// belongs to a PREVIOUS turn, and the first user prompt after the last such part is
// this turn's seed. Later user prompts are mid-turn interjections that piled up before
// execution began. Returns -1 when the log has no genuine user prompt (e.g. a subagent
// session, whose seed is authored by ActorAgent).
func seedPromptIdx(evs []event.Event) int {
	abandoned := abandonedPromptIDs(evs) // prompts whose turn was cancelled — resolved, never a seed
	ui := -1                             // running index into userPromptEntries order
	lastAnswered := -1                   // highest user-prompt index a prior assistant reply covered
	for _, e := range evs {
		switch {
		case e.Type == event.TypePromptSubmitted && e.Actor.Kind == event.ActorUser:
			ui++
			// A cancelled (abandoned) prompt counts as resolved: skip it so the next
			// genuine prompt seeds the turn instead of the dead one.
			var d event.PromptSubmittedData
			if json.Unmarshal(e.Data, &d) == nil && abandoned[d.MessageID] {
				lastAnswered = ui
			}
		case e.Type == event.TypePartAppended && e.Actor.Kind == event.ActorAgent:
			if ui >= 0 {
				lastAnswered = ui
			}
		}
	}
	if ui < 0 {
		return -1
	}
	seed := lastAnswered + 1
	if seed > ui {
		seed = ui // defensive: everything already answered → treat the latest as seed
	}
	return seed
}

// abandonedPromptIDs returns the set of user-prompt MessageIDs marked abandoned (their
// turn was cancelled before answering — TypePromptAbandoned). seedPromptIdx uses it to
// skip a cancelled prompt so it cannot seed a later, unrelated turn.
func abandonedPromptIDs(evs []event.Event) map[string]bool {
	var out map[string]bool
	for _, e := range evs {
		if e.Type == event.TypePromptAbandoned {
			var d event.PromptAbandonedData
			if json.Unmarshal(e.Data, &d) == nil && d.MsgID != "" {
				if out == nil {
					out = map[string]bool{}
				}
				out[d.MsgID] = true
			}
		}
	}
	return out
}

// promptUnanswered reports whether the user prompt with msgID exists in the log and has
// no assistant (ActorAgent) reply after it. Used to guard the cancel-abandon path against
// a stale seed reference: a prompt that already produced an answer must not be abandoned.
func promptUnanswered(evs []event.Event, msgID string) bool {
	seen := false
	for _, e := range evs {
		switch {
		case e.Type == event.TypePromptSubmitted && e.Actor.Kind == event.ActorUser:
			var d event.PromptSubmittedData
			if json.Unmarshal(e.Data, &d) == nil && d.MessageID == msgID {
				seen = true
			}
		case e.Type == event.TypePartAppended && e.Actor.Kind == event.ActorAgent:
			if seen {
				return false // an assistant reply landed after the prompt → answered
			}
		}
	}
	return seen
}

// userPromptEntries returns every genuine user (ActorUser) prompt in log order, each
// paired with its PromptSubmitted MessageID.
func userPromptEntries(evs []event.Event) []userPrompt {
	var out []userPrompt
	for _, e := range evs {
		if e.Type == event.TypePromptSubmitted && e.Actor.Kind == event.ActorUser {
			var d event.PromptSubmittedData
			if json.Unmarshal(e.Data, &d) == nil {
				out = append(out, userPrompt{MsgID: d.MessageID, Text: partsText(d.Parts)})
			}
		}
	}
	return out
}

// currentTaskText is the query for push-side shard hints: the most recent genuine
// user prompt (what the user asked) joined with the latest assistant message (what
// the agent is doing right now). Using both means a hint can fire on the file the
// agent just started editing, not only on words from the opening prompt — the case
// where a weak model has drifted onto a sub-task and most needs the recalled detail.
func currentTaskText(evs []event.Event) string {
	prompt := lastUserPromptText(evs)
	var last string
	msgs := reconstruct(evs)
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == session.RoleAssistant {
			var b strings.Builder
			for _, p := range msgs[i].Parts {
				if p.Kind == session.PartText {
					b.WriteString(p.Text + " ")
				}
			}
			if s := strings.TrimSpace(b.String()); s != "" {
				last = s
				break
			}
		}
	}
	return strings.TrimSpace(prompt + " " + last)
}

// lastUserText returns the text of the most recent user message.
func lastUserText(msgs []session.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == session.RoleUser {
			var b strings.Builder
			for _, p := range msgs[i].Parts {
				if p.Kind == session.PartText {
					b.WriteString(p.Text)
				}
			}
			return b.String()
		}
	}
	return ""
}

// experiencePointer renders the one-line push notice: how many stored memories/skills
// match, pointing the agent at recall_memory to pull the detail. Empty when nothing
// matched, so the section is dropped entirely.
func experiencePointer(nMem, nSkill int) string {
	total := nMem + nSkill
	if total == 0 {
		return ""
	}
	noun := "entry"
	if total != 1 {
		noun = "entries"
	}
	return fmt.Sprintf("%d relevant team memory/skill %s exist — call recall_memory with keywords to read the details.", total, noun)
}

// formatExperienceFull renders the full memory/skill entries for a recall_memory pull
// (as opposed to the one-line push pointer). This is where the detail actually enters
// context, and only when the agent asked for it.
func formatExperienceFull(mems []port.Memory, skills []port.Skill) string {
	var b strings.Builder
	for _, m := range mems {
		b.WriteString("- " + strings.TrimSpace(m.Text) + "\n")
	}
	for _, s := range skills {
		b.WriteString("- skill " + s.Name + ": " + strings.TrimSpace(s.Description) + "\n")
		if body := strings.TrimSpace(s.Body); body != "" {
			b.WriteString("  " + strings.ReplaceAll(body, "\n", "\n  ") + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// formatTodos renders the plan as a checklist for the system prompt.
func formatTodos(td []session.Todo) string {
	mark := map[string]string{"completed": "[x]", "in_progress": "[~]", "pending": "[ ]", "cancelled": "[✗]"}
	var b strings.Builder
	for i, t := range td {
		if i > 0 {
			b.WriteString("\n")
		}
		m := mark[t.Status]
		if m == "" {
			m = "[ ]"
		}
		b.WriteString(m + " " + t.Content)
	}
	return b.String()
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
