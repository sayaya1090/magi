package git

import "strings"

// A skill may open with a small YAML-ish header. It does not have to.
//
// The format before this was "first line is the description, the rest is the body", and every
// skill on disk is written that way. A header is therefore OPTIONAL and recognised only by an
// opening `---` line: a file without one parses exactly as it always did. That is not politeness
// about old files, it is the difference between an upgrade and a silent disappearance — a skill
// that stops matching stops being retrieved, and nothing anywhere says why.
//
// The parser is deliberately small. It reads `key: value` and nothing else — no nesting, no
// anchors, no multi-line scalars — because the alternative is a YAML dependency for two fields,
// and a header that can express more than the host reads is a place for expectations to collect.
// A line it cannot parse is skipped rather than failing the file: a skill is content, and refusing
// to load one over a stray line in its header would lose the content to protect the metadata.
//
// # agent-groups
//
// The field this exists for. A skill names the AGENT GROUPS it serves:
//
//	---
//	description: Review a diff through a security lens
//	agent-groups: [review, security]
//	---
//
// An agent declares the groups it belongs to, and a skill is offered to it when the two sets
// intersect. The two sides share one namespace and are named from where you are standing: the
// skill says which agent groups it is for, the agent says which groups it is in.
//
// A skill with NO agent-groups is offered to everyone. That is what keeps every skill written
// before this field existed working, and it makes restriction something a skill opts into rather
// than something every author must remember to undo.

// skillHeader is what a skill's optional header can say.
type skillHeader struct {
	Description string
	AgentGroups []string
}

// parseSkill splits a skill file into its header, description and body.
//
// With a `---` header: description comes from the header's `description:` key, and the body is
// everything after the closing `---`. Without one: the first line is the description and the rest
// is the body, exactly as before.
func parseSkill(text string) (skillHeader, string) {
	rest, ok := strings.CutPrefix(strings.TrimLeft(text, "\n"), "---\n")
	if !ok {
		desc, body := splitFirstLine(text)
		return skillHeader{Description: desc}, body
	}
	head, body, closed := strings.Cut(rest, "\n---")
	if !closed {
		// An opening marker with no closing one is not a header — it is a body that happens to
		// start with a rule. Reading it as a header would swallow the whole skill.
		desc, b := splitFirstLine(text)
		return skillHeader{Description: desc}, b
	}
	h := skillHeader{}
	for _, line := range strings.Split(head, "\n") {
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key, val = strings.ToLower(strings.TrimSpace(key)), strings.TrimSpace(val)
		switch key {
		case "description":
			h.Description = val
		case "agent-groups":
			h.AgentGroups = parseList(val)
		}
	}
	return h, strings.TrimSpace(strings.TrimPrefix(body, "\n---"))
}

// parseList reads `[a, b]` or `a, b` or `a b` into a slice. Three spellings because a header is
// written by hand and being strict about the brackets would only produce empty lists nobody
// notices.
func parseList(v string) []string {
	v = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(v, "["), "]"))
	if v == "" {
		return nil
	}
	f := func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }
	var out []string
	for _, p := range strings.FieldsFunc(v, f) {
		if p = strings.Trim(strings.TrimSpace(p), `"'`); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// visibleTo reports whether a skill with these groups is offered to an agent in those groups.
//
// The two defaults are the whole compatibility story, and they point in opposite directions on
// purpose:
//
//   - A skill with no groups is offered to EVERYONE. Every skill written before this field existed
//     has none, so the alternative is that upgrading hides all of them.
//   - An agent with no groups is offered only UNGROUPED skills. The other way round, labelling a
//     skill would not shrink anyone's context, which is the entire reason to label one.
//
// Together they change nothing today — no skill carries a group, so every agent sees every skill,
// exactly as before — and they start narrowing the moment somebody labels one.
//
// "*" on the agent side means everything, for a generalist that wants the whole shelf.
func visibleTo(skillGroups, agentGroups []string) bool {
	if len(skillGroups) == 0 {
		return true
	}
	for _, a := range agentGroups {
		if a == "*" {
			return true
		}
		for _, s := range skillGroups {
			if strings.EqualFold(a, s) {
				return true
			}
		}
	}
	return false
}
