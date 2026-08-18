// Package report is the shape of what an agent hands a person when the person has to decide.
//
// # Why this exists
//
// Until now a decision reached the console as a question and two to four option labels. "Which
// branch should this land on?" with [main] [engine-ui-split] — and nothing about what had been
// tried, what either choice costs, or what the agent would pick. The person was asked to decide
// with no grounds, from a machine that had spent an hour accumulating exactly those grounds and
// then dropped them.
//
// # Why a skill defines it and not a config field
//
// What belongs in a report is a matter of taste, and taste differs per person and per kind of work
// — a design companion's report is not an ops companion's. Skills already carry that shape: they
// are prose the agent is told to follow, and the store already has a global tier and a per-workspace
// tier. So "each agent may define its own report" is not a feature to build; it is what happens when
// a companion's workspace carries its own copy of the skill. Nothing new stores anything.
//
// # Why magi checks it rather than trusting the prose
//
// A skill that says "write what you tried" is advice, and this tree has repeatedly found advice
// evaporating under pressure: prompts that asked for a thing and got it four times out of five, with
// nobody able to say which times. So the skill's sections become a CONTRACT — the model fills named
// fields, and the tool refuses a report with a section missing or blank, naming what is absent. The
// model supplies data; magi decides whether the data is a report. That is the same division the
// typed checks arrived at after the authored-shell-check failure.
package report

import (
	"fmt"
	"sort"
	"strings"
)

// SkillName is the skill a report's shape is read from. A companion with its own copy in its
// workspace gets its own report; everyone else gets the global one; with neither, Default applies.
const SkillName = "decision-report"

// Section is one named part of a report: the key the agent fills, and the sentence telling it what
// belongs there. The prompt is carried around because it is what a rejection can quote back — an
// error that says "tried is missing" teaches less than one that also says what tried is for.
type Section struct {
	Key    string
	Prompt string
}

// Contract is the ordered set of sections a report must carry.
type Contract []Section

// Default is what a report is when nobody has said otherwise.
//
// Three sections, because a person deciding needs three things and this tree has watched all three
// go missing: what the agent actually did (so the reader is not re-deriving it), what each way costs
// (so the reader knows which mistakes are cheap), and which way the agent would go (so the reader
// can disagree with something concrete rather than start from nothing).
var Default = Contract{
	{"tried", "what you actually ran or read, and what it showed"},
	{"stakes", "what each option costs if it turns out to be the wrong one"},
	{"lean", "which one you would pick, and the reason"},
}

// Parse reads a contract out of a skill body.
//
// The form is deliberately small: a "## sections" heading, then one "- key: what belongs here" per
// line. A skill is prose meant for a model to read, and anything richer would be a schema hidden in
// a document — the person editing it would have to learn a format to change a sentence.
//
// Prose before and after the block is ignored, so the skill can explain itself to the agent at any
// length. A body with no section block yields nil, and the caller falls back to Default: a skill
// that forgot to declare sections must not silently mean "a report needs nothing".
func Parse(body string) Contract {
	var out Contract
	seen := map[string]bool{}
	in := false
	for _, line := range strings.Split(body, "\n") {
		t := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(t, "#"):
			// Any heading ends the block, so a "## sections" list cannot swallow the rest of the
			// document when somebody forgets a blank line.
			in = strings.EqualFold(strings.TrimSpace(strings.TrimLeft(t, "# ")), "sections")
			continue
		case !in, t == "":
			continue
		case !strings.HasPrefix(t, "- "):
			in = false // the list ended; prose resumed
			continue
		}
		key, prompt, ok := strings.Cut(strings.TrimPrefix(t, "- "), ":")
		key = strings.ToLower(strings.TrimSpace(key))
		if !ok || key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, Section{Key: key, Prompt: strings.TrimSpace(prompt)})
	}
	return out
}

// Filled is one answered section on its way to a screen: the key the contract named, and what the
// agent wrote for it. Separate from Section because a contract is a question and this is an answer
// — carrying the prompt alongside the text would put the instruction on the reader's screen.
type Filled struct {
	Key  string `json:"key"`
	Text string `json:"text"`
}

// Fill orders a report for reading: the contract's sections first, in the order the contract asked
// for them, then anything extra the agent wrote, alphabetically.
//
// Ordered rather than a map, because the order IS part of the report — a contract that asks what
// you tried before what you would pick is describing how the reader should arrive at the decision,
// and a map hands that back as whatever the runtime felt like.
func (c Contract) Fill(values map[string]string) []Filled {
	out := make([]Filled, 0, len(values))
	for _, s := range c {
		if t := strings.TrimSpace(values[s.Key]); t != "" {
			out = append(out, Filled{Key: s.Key, Text: t})
		}
	}
	for _, k := range c.Unknown(values) {
		out = append(out, Filled{Key: k, Text: strings.TrimSpace(values[k])})
	}
	return out
}

// Missing names the sections a report does not answer, in the contract's order.
//
// Whitespace counts as absent. A section filled with a space is a section the model skipped while
// satisfying the letter of the check, and this tree has been handed that exact shape before.
func (c Contract) Missing(values map[string]string) []string {
	var out []string
	for _, s := range c {
		if strings.TrimSpace(values[s.Key]) == "" {
			out = append(out, s.Key)
		}
	}
	return out
}

// Unknown names keys the report carries that the contract did not ask for, sorted.
//
// Not an error — a report that says more than it was asked to is doing its job. It is returned so a
// caller can keep them in a stable order when rendering, rather than dropping them because they were
// not on a list.
func (c Contract) Unknown(values map[string]string) []string {
	want := map[string]bool{}
	for _, s := range c {
		want[s.Key] = true
	}
	var out []string
	for k := range values {
		if !want[k] && strings.TrimSpace(values[k]) != "" {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// Spec is the contract written out for the agent to read: what to fill, and what belongs in each.
// It is what a rejection says, so one refused call is enough to learn the shape.
func (c Contract) Spec() string {
	var b strings.Builder
	for _, s := range c {
		if s.Prompt == "" {
			fmt.Fprintf(&b, "\n  %s", s.Key)
			continue
		}
		fmt.Fprintf(&b, "\n  %s — %s", s.Key, s.Prompt)
	}
	return b.String()
}
