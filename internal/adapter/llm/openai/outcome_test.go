package openai

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
)

func TestModelReceivesToolOutcomeOnNormalAndOrphanResults(t *testing.T) {
	for _, orphan := range []bool{false, true} {
		for _, tc := range []struct {
			name             string
			failed, advisory bool
			prefix           string
		}{
			{"success", false, false, ""},
			{"failure", true, false, "[tool outcome: error; partial side effects may have occurred]\n"},
			{"post-edit diagnostics", true, true, "[tool outcome: operation succeeded; follow-up attention required]\n"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				raw, _ := json.Marshal("  original output\n")
				result := session.Message{Role: session.RoleTool, Parts: []session.Part{{Kind: session.PartToolResult, ToolResult: &session.ToolResult{CallID: "c1", Content: raw, IsError: tc.failed, Advisory: tc.advisory}}}}
				msgs := []session.Message{result}
				if !orphan {
					msgs = append([]session.Message{asstCall("c1", "write")}, msgs...)
				}
				wire := convertMessages(msgs, false)
				encoded, _ := json.Marshal(wire)
				want := tc.prefix + "  original output\n"
				if orphan {
					want = "[tool result] " + want
				}
				quoted, _ := json.Marshal(want)
				if !strings.Contains(string(encoded), string(quoted)) {
					t.Fatalf("outcome missing (orphan=%v): %s", orphan, encoded)
				}
			})
		}
	}
}
