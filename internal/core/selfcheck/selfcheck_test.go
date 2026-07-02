package selfcheck

import "testing"

func TestFabricationMarker(t *testing.T) {
	// A marker is found case-insensitively and the containing line is returned trimmed.
	m, line := FabricationMarker("real code here\n  In A Real Implementation this would run it\nmore")
	if m == "" {
		t.Fatal("expected a marker hit")
	}
	if line != "in a real implementation this would run it" {
		t.Errorf("excerpt = %q", line)
	}

	// Clean text yields no hit.
	if m, line := FabricationMarker("solved it by running the actual program and reading its output"); m != "" || line != "" {
		t.Errorf("clean text flagged: marker=%q line=%q", m, line)
	}

	// A very long line is bounded.
	long := "since we cannot " + string(make([]byte, 400))
	if _, line := FabricationMarker(long); len(line) > 170 {
		t.Errorf("excerpt not bounded: len=%d", len(line))
	}
}
