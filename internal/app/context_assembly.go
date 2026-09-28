package app

import "strings"

// assembleContext preserves caller order and exact bytes. Collection, cache lifetimes,
// events and tool execution belong to callers. A nil tool set leaves filtering disabled.
func assembleContext(parts []contextFragment, tools map[string]bool) (string, []contextDecision) {
	var out strings.Builder
	decisions := make([]contextDecision, 0, len(parts))
	for _, part := range parts {
		d := contextDecision{ID: part.id, Lane: part.lane, Bytes: len(part.text), State: "included"}
		if tools != nil {
			for _, name := range part.requires {
				if !tools[name] {
					d.State, d.Reason = "skipped", "missing_tool"
					break
				}
			}
		}
		decisions = append(decisions, d)
		if d.State == "included" {
			out.WriteString(part.text)
		}
	}
	return out.String(), decisions
}
