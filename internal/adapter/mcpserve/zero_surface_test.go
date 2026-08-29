package mcpserve

import "testing"

func TestFirstLine(t *testing.T) {
	if got := firstLine("a\nb\nc"); got != "a" {
		t.Fatalf("got %q", got)
	}
	if got := firstLine("solo"); got != "solo" {
		t.Fatalf("got %q", got)
	}
}
