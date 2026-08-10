package companion

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// Saying what an answer from another companion was worth.
//
// # Why the agent is asked rather than magi working it out
//
// Everything magi can see about a hand-off is that it came back. Whether it was the answer needed
// is a judgement about content, and the only reader that had the question, the answer and the work
// they were both for is the turn that asked. Nothing else is in a position to say it.
//
// It is a judgement of somebody ELSE's work, which is what makes it admissible: this tree does not
// take an agent's word for its own results, and this is not that. See record.go.
//
// # Recorded, never acted on
//
// The verdict goes into a file and into the description of the next hand_off. It does not reorder
// the roster, and hand_off does not consult it. A magi that quietly preferred yesterday's best
// companion would be making the choice while appearing to offer one.
type Rate struct{}

func (Rate) Name() string { return "rate_handoff" }

func (Rate) Description() string {
	return "Say what a companion's answer was worth, after you have used it (or found you could " +
		"not). One call per companion you heard back from.\n\n" +
		"This is a record for whoever chooses next — including you, later. It changes nothing " +
		"about who work goes to now, and nobody is ranked by it. Judge the ANSWER, not whether " +
		"it arrived: an answer that came back quickly and was not what you asked for is a " +
		"\"missed\", and a slow one you used is a \"good\"."
}

func (Rate) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{
		"who":{"type":"string","description":"the companion whose answer you are judging, as they were named when it arrived"},
		"verdict":{"type":"string","enum":["good","missed"],"description":"good — you used it; missed — it came back but was not what you needed"},
		"why":{"type":"string","description":"one sentence, for whoever reads this later: what made it useful, or what was missing"},
		"asked":{"type":"string","description":"optional: what you had asked them, so the record says what the verdict was about"}
	},"required":["who","verdict","why"],"additionalProperties":false}`)
}

func (r Rate) Execute(_ context.Context, args json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	var in struct {
		Who     string `json:"who"`
		Verdict string `json:"verdict"`
		Why     string `json:"why"`
		Asked   string `json:"asked"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return errText("invalid arguments: " + err.Error()), nil
	}
	in.Who, in.Why, in.Asked = strings.TrimSpace(in.Who), strings.TrimSpace(in.Why), strings.TrimSpace(in.Asked)
	in.Verdict = strings.ToLower(strings.TrimSpace(in.Verdict))
	switch {
	case in.Who == "":
		return errText("name the companion whose answer you are judging"), nil
	case in.Verdict != VerdictGood && in.Verdict != VerdictMissed:
		return errText(fmt.Sprintf("the verdict is %q or %q, not %q", VerdictGood, VerdictMissed,
			in.Verdict)), nil
	case in.Why == "":
		// Required, because a column of names and adjectives is not a record. The one line is what
		// makes it readable to whoever opens the file a month later, including this agent.
		return errText("say in one sentence what made it useful, or what was missing — a verdict " +
			"with no reason is not something anybody can act on later"), nil
	case strings.TrimSpace(env.Workdir) == "":
		return errText("this magi has no workspace to record that in"), nil
	}
	if err := Record(env.Workdir, Verdict{
		Who: in.Who, Verdict: in.Verdict, Why: in.Why, Asked: fleetClip(in.Asked, 240),
	}); err != nil {
		return errText("could not write it down: " + err.Error()), nil
	}
	return okText(fmt.Sprintf("Recorded: %s — %s. It will be shown next time somebody here is "+
		"choosing who to hand work to, and it changes nothing else.", in.Who, in.Verdict)), nil
}

// fleetClip keeps a quoted request from turning the record into a transcript.
func fleetClip(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
