package event

import "testing"

func TestCompactionSummaryErrorIsNarrow(t *testing.T) {
	for _, s := range []string{"Prompt is too long · A single-exchange conversation cannot be compacted; reduce attached files/tools.", " [claude: backend refused] ", "[claude exited before answering]"} {
		if !CompactionSummaryError(s) {
			t.Fatalf("accepted transport failure: %q", s)
		}
	}
	for _, s := range []string{"", "The build failed; next fix the compiler error.", "The agent investigated 'Prompt is too long' and fixed request budgeting.", "Prompt is too long is the title of the regression test."} {
		if CompactionSummaryError(s) {
			t.Fatalf("rejected ordinary summary: %q", s)
		}
	}
}
