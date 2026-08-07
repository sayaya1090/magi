package app

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// What a person had to step in and say.
//
// A supervisor's time only scales if each intervention permanently removes future ones — that is
// the whole economics of one person holding several companions. The material for that is the set of
// moments a human interrupted the work, and until now nothing collected them: a steer is an ordinary
// user prompt and dissolves into the transcript, indistinguishable from a new instruction.
//
// # Derived, not recorded
//
// No new event and no producer change. A user prompt that arrives while a turn is still OPEN is a
// steer by construction — the person did not wait for the answer, which is what "stepping in" means
// — and the log already says which prompts those are: any prompt.submitted with no turn.finished
// between it and the previous one. A denial is the other kind, and permission.decided already
// carries it.
//
// Deriving beats recording here for the reason this tree keeps relearning: a field added today
// describes nothing that happened yesterday, and every log already on disk would answer "no
// interventions" for the period the supervisor most wants to look at. This reads them all.
//
// # What it does NOT decide
//
// Whether an intervention was a CORRECTION. "also add tests" is an addition; "no, not that file" is
// a correction; magi cannot tell them apart and guessing would put words in a person's mouth and
// then promote them into a rule. The list is offered and the human picks — the same division as
// everywhere else here: magi supplies the footprint, the person supplies the judgement.

// Intervention is one moment a person stepped into a running turn.
type Intervention struct {
	Kind    string    // "steer" — said mid-turn; "denied" — refused a tool
	Text    string    // what they said, or the tool they refused
	At      time.Time //
	Session session.SessionID
	// Answered is how long the turn had been running when it arrived, in seconds. A steer three
	// seconds in is a correction to the instruction; one twenty minutes in is a correction to the
	// work, and they promote to different things.
	AfterSec int
}

// Interventions returns the moments a person stepped in, oldest first.
func (a *App) Interventions(ctx context.Context, sid session.SessionID) ([]Intervention, error) {
	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		return nil, err
	}
	return interventions(evs, sid), nil
}

func interventions(evs []event.Event, sid session.SessionID) []Intervention {
	var out []Intervention
	open := false          // a turn is running
	var openedAt time.Time // when it started
	for _, e := range evs {
		switch e.Type {
		case event.TypePromptSubmitted:
			var d event.PromptSubmittedData
			if json.Unmarshal(e.Data, &d) != nil {
				continue
			}
			text := strings.TrimSpace(promptText(d))
			// Only a person's own words. magi injects prompts of its own — nudges, resurfaced
			// interjections, the finish-declaration reminder — and counting those as interventions
			// would have the supervisor promoting magi's advice to itself into a rule.
			if e.Actor.Kind != event.ActorUser || text == "" {
				if !open {
					open, openedAt = true, e.TS
				}
				continue
			}
			if open {
				out = append(out, Intervention{
					Kind: "steer", Text: text, At: e.TS, Session: sid,
					AfterSec: secondsBetween(openedAt, e.TS),
				})
				continue // the turn stays open; a steer does not start a new one
			}
			open, openedAt = true, e.TS
		case event.TypeTurnFinished, event.TypeError, event.TypePromptAbandoned:
			open = false
		case event.TypePermissionDecided:
			var d event.PermissionDecidedData
			if json.Unmarshal(e.Data, &d) != nil {
				continue
			}
			// Only a refusal. An allow is the ordinary course of a turn; a deny is a person saying
			// "not that", which is the shortest correction there is.
			if d.Decision != "deny" {
				continue
			}
			out = append(out, Intervention{
				Kind: "denied", Text: d.CallID, At: e.TS, Session: sid,
				AfterSec: secondsBetween(openedAt, e.TS),
			})
		}
	}
	return out
}

func secondsBetween(from, to time.Time) int {
	if from.IsZero() || to.IsZero() || to.Before(from) {
		return 0
	}
	return int(to.Sub(from).Seconds())
}
