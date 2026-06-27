package builtin

import (
	"encoding/json"

	"github.com/sayaya1090/magi/internal/core/session"
)

// errResult builds a failed ToolResult with a message.
func errResult(callID, msg string) session.ToolResult {
	b, _ := json.Marshal(msg)
	return session.ToolResult{CallID: callID, Content: b, IsError: true}
}

// okText builds a successful ToolResult carrying a string payload.
func okText(callID, s string) session.ToolResult {
	b, _ := json.Marshal(s)
	return session.ToolResult{CallID: callID, Content: b}
}

// okJSON builds a successful ToolResult carrying an arbitrary JSON payload.
func okJSON(callID string, v any) session.ToolResult {
	b, _ := json.Marshal(v)
	return session.ToolResult{CallID: callID, Content: b}
}

// isBinary reports whether b looks like binary data (contains a NUL byte).
func isBinary(b []byte) bool {
	for _, c := range b {
		if c == 0 {
			return true
		}
	}
	return false
}
