package app

import (
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
	"strings"
	"unicode/utf8"
)

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

func contextToolNames(specs []port.ToolSpec) map[string]bool {
	names := make(map[string]bool, len(specs))
	for _, spec := range specs {
		names[spec.Name] = true
	}
	return names
}

// Preserve a valid UTF-8 boundary without increasing the existing byte budget.
func truncateContextBytes(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	for limit > 0 && !utf8.RuneStart(s[limit]) {
		limit--
	}
	return s[:limit]
}

// contextDiagnostics returns metadata only; callers cannot mutate the stored decisions.
func (a *App) contextDiagnostics(sid session.SessionID) []contextDecision {
	a.mu.Lock()
	defer a.mu.Unlock()
	st := a.stateLocked(sid)
	out := append([]contextDecision(nil), st.systemDecisions...)
	out = append(out, st.contextDecisions...)
	out = append(out, st.arrivalDecisions...)
	return append(out, st.ragDecisions...)
}
