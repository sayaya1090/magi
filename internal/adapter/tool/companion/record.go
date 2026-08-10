package companion

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// What came of asking somebody, written down where the next choice can read it.
//
// # It records; it does not decide
//
// Nothing here reorders a roster, filters a candidate, or scores anybody. The model picks, and this
// puts one more fact in front of it — the same relationship the roster itself has to the choice.
// A magi that quietly sorted its companions by past results would be making the delegation decision
// while appearing to offer one, and the failure that produced hand_off in the first place was
// exactly a mechanism deciding on the model's behalf who work should go to.
//
// # The verdict is usefulness, not delivery
//
// Whether an answer arrived is already known: the wait reports it, and a companion that died is
// reported as such. What nothing knows is whether the answer was the one that was needed, and that
// is the only judgement worth keeping — a peer that answers promptly and off-target is worse to
// choose than one that takes longer and lands.
//
// It is a judgement of somebody ELSE's work, which is why an agent may make it. This tree refuses
// self-assessment as evidence; rating a peer sits where the council sits, and the thing being
// judged is in the transcript for anybody to check.
//
// # Append-only, one line each, in the workspace
//
// Committable on purpose: who a team's work should go to is a fact about that team, it survives a
// machine, and it belongs beside the code the way a skill does. Append-only so two turns writing at
// once cannot lose each other's line, and so a diff shows what was added rather than a rewritten
// file.

// recordFile is where a workspace keeps what it thought of the answers it got.
func recordFile(workdir string) string {
	return filepath.Join(workdir, ".magi", "handoffs.jsonl")
}

// Verdicts are the two answers worth telling apart. Two and not five: a scale invites a model to
// average, and what the next choice needs is "did this land" rather than a score.
const (
	VerdictGood   = "good"
	VerdictMissed = "missed"
)

// Verdict is one recorded opinion of one answer.
type Verdict struct {
	At      time.Time `json:"at"`
	Who     string    `json:"who"`
	Verdict string    `json:"verdict"`
	Why     string    `json:"why,omitempty"`
	// Asked is what they were asked, clipped. Kept so the file reads as a record of decisions
	// rather than a column of names and adjectives — a person opening it a month later needs to
	// know what "missed" was about.
	Asked string `json:"asked,omitempty"`
}

// Record appends one verdict.
func Record(workdir string, v Verdict) error {
	if strings.TrimSpace(workdir) == "" {
		return fmt.Errorf("no workspace to record it in")
	}
	if v.At.IsZero() {
		v.At = time.Now().UTC()
	}
	line, err := json.Marshal(v)
	if err != nil {
		return err
	}
	dir := filepath.Dir(recordFile(workdir))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// O_APPEND, and one write: two turns rating at the same moment each land a whole line rather
	// than interleaving, which is the whole reason this is a log and not a document.
	f, err := os.OpenFile(recordFile(workdir), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}

// Tally renders what has been recorded, for the description a model chooses from.
//
// Counts, with the denominator. "three of four useful" and "one of one useful" are different facts
// and a percentage hides which one you are looking at — a companion asked once is not a companion
// with a record. Nobody is ranked and nobody is left out for having a bad line; a name absent from
// this list has simply never been rated, which is said rather than implied.
func Tally(workdir string) string {
	kept := readVerdicts(workdir)
	if len(kept) == 0 {
		return ""
	}
	type count struct{ good, all int }
	by := map[string]*count{}
	var order []string
	for _, v := range kept {
		name := strings.TrimSpace(v.Who)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		c, seen := by[key]
		if !seen {
			c = &count{}
			by[key] = c
			order = append(order, name)
		}
		c.all++
		if v.Verdict == VerdictGood {
			c.good++
		}
	}
	if len(order) == 0 {
		return ""
	}
	sort.Slice(order, func(i, j int) bool {
		// By name. Deliberately NOT by score: a list in rank order is a recommendation, and this
		// is a record.
		return strings.ToLower(order[i]) < strings.ToLower(order[j])
	})
	var b strings.Builder
	b.WriteString("How work you handed over before turned out, as you judged it at the time. " +
		"A record, not a ranking — and a name that is not here has simply never been rated, " +
		"which is not the same as having been rated badly:\n")
	for _, name := range order {
		c := by[strings.ToLower(name)]
		fmt.Fprintf(&b, "  %s — %d of %d useful\n", name, c.good, c.all)
	}
	return b.String()
}

// readVerdicts reads the log, skipping lines it cannot parse.
//
// A half-written or hand-edited line loses itself and nothing else. The alternative — refusing the
// whole file — would turn one bad line into a workspace with no record at all, and this is a thing
// people are meant to open and edit.
func readVerdicts(workdir string) []Verdict {
	b, err := os.ReadFile(recordFile(workdir))
	if err != nil {
		return nil
	}
	var out []Verdict
	for _, line := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var v Verdict
		if json.Unmarshal([]byte(line), &v) != nil {
			continue
		}
		out = append(out, v)
	}
	return out
}
