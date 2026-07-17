package builtin

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/sayaya1090/magi/internal/core/session"
)

// flexInt is an integer tool argument that tolerates the shapes weak models
// actually emit: 300, 300.0, "300", "300.000000", "300s". Strict int fields
// rejected the WHOLE tool call over a field's type ("cannot unmarshal string
// into … type int") — observed live: the model then abandoned the action
// (skipped reading a file it needed and proceeded on assumption) instead of
// correcting the type. An unparseable value falls back to 0 (= the field's
// unset/default semantics) rather than failing the call: these fields are
// bounds and positions, never the point of the call.
type flexInt int

// flexBool is a boolean tool argument with the same tolerance rationale as
// flexInt: weak models emit "true"/"false" (and occasionally 1/0) where the
// schema says boolean, and a strict bool rejected the whole call over it.
// Junk falls back to false (= the field's unset/default semantics).
type flexBool bool

func (v *flexBool) UnmarshalJSON(b []byte) error {
	switch strings.ToLower(strings.TrimSpace(strings.Trim(string(b), `"`))) {
	case "true", "yes", "on", "1":
		*v = true
	default:
		*v = false // false, junk, null — all mean "not set"
	}
	return nil
}

func (v *flexInt) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(strings.Trim(string(b), `"`))
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(s, "sec"), "s"))
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		*v = flexInt(int(f))
	} else {
		*v = 0 // unparseable → unset/default, never a rejected call
	}
	return nil
}

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
