package event

import "strings"

// CompactionSummaryError recognizes transport errors that older CLI shims sent
// as ordinary assistant text. Keep this narrow: a summary discussing an error
// must not be rejected merely for mentioning its wording.
func CompactionSummaryError(summary string) bool {
	s := strings.TrimSpace(summary)
	lower := strings.ToLower(s)
	return (strings.HasPrefix(lower, "prompt is too long") && strings.Contains(lower, "single-exchange conversation cannot be compacted")) ||
		strings.HasPrefix(lower, "[claude: ") || s == "[claude exited before answering]"
}
