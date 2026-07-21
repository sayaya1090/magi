package app

import "testing"

// coalesceInterjectionText merges a batch into one prompt: distinct non-empty texts in order,
// de-duplicated (a re-typed identical question collapses), joined by newlines.
func TestCoalesceInterjectionText(t *testing.T) {
	got := coalesceInterjectionText([]pendingInterjection{
		{MsgID: "m1", Text: "how's it going"},
		{MsgID: "m2", Text: "?"},
		{MsgID: "m3", Text: "  "},             // blank dropped
		{MsgID: "m4", Text: "how's it going"}, // duplicate collapsed
		{MsgID: "m5", Text: "다시"},
	})
	want := "how's it going\n?\n다시"
	if got != want {
		t.Fatalf("coalesce = %q, want %q", got, want)
	}
	if coalesceInterjectionText(nil) != "" {
		t.Error("empty batch → empty text")
	}
	if coalesceInterjectionText([]pendingInterjection{{Text: "  "}, {Text: ""}}) != "" {
		t.Error("all-blank batch → empty text")
	}
}
