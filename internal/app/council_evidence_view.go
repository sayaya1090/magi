package app

import (
	"context"
	"encoding/json"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// What the council was shown, for a surface that wants to check a verdict rather than read it.
//
// A vote on its own can only be weighed: the member says the work is done and you either believe
// it or you do not. Beside the material it judged it can be CHECKED — the claim against the diff,
// the diff against the task. The terminal has led its detail view with this since it had one; the
// console had no way to ask for it, so its council rows were a verdict and nothing behind it.
//
// Read out of the log rather than kept anywhere: the round announces its own evidence when it
// convenes, which is the only moment anything knows what was about to be shown.

// CouncilEvidence is one round's material, as the members received it.
type CouncilEvidence struct {
	Round int `json:"round"`
	// Members and Rule say who was asked and how the answer was to be counted. A verdict read
	// without them is a vote with no electorate.
	Members []string `json:"members,omitempty"`
	Rule    string   `json:"rule,omitempty"`
	Task    string   `json:"task,omitempty"`
	Plan    string   `json:"plan,omitempty"`
	Report  string   `json:"report,omitempty"`
	// Actions is what the turn's tools produced — the one part that is neither the request, nor
	// the agent's account of it, nor a reconstruction.
	Actions string `json:"actions,omitempty"`
	Changes string `json:"changes,omitempty"`
	// NoChanges distinguishes a read-only turn from one whose diff was simply not recorded, which
	// otherwise look identical: both are an empty Changes.
	NoChanges bool `json:"noChanges,omitempty"`
	// Asked records that this round asked its members for a "keep". Without it an empty Keep on
	// every verdict is ambiguous — nobody was asked, or everyone was and none answered.
	Asked bool `json:"asked,omitempty"`
}

// CouncilEvidenceOf returns what a round was shown, and whether that round happened at all.
//
// The LAST convening of that round number wins. A round can be convened again after a rebuttal,
// and what the members saw the second time is what the verdicts being read were standing on.
func (a *App) CouncilEvidenceOf(ctx context.Context, sid session.SessionID, round int) (CouncilEvidence, bool, error) {
	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		return CouncilEvidence{}, false, err
	}
	out, found := CouncilEvidence{}, false
	for _, e := range evs {
		if e.Type != event.TypeCouncilConvened {
			continue
		}
		var d event.CouncilConvenedData
		if json.Unmarshal(e.Data, &d) != nil || d.Round != round {
			continue
		}
		out, found = CouncilEvidence{
			Round: d.Round, Members: d.Members, Rule: d.Rule,
			Task: d.Task, Plan: d.Plan, Report: d.Report, Actions: d.Actions,
			Changes: d.Changes, NoChanges: d.NoChanges, Asked: d.Keep,
		}, true
	}
	return out, found, nil
}
