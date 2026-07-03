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

func TestTestArtifactPath(t *testing.T) {
	yes := []string{
		"pkg/server_test.go", "tests/integration.py", "src/__mocks__/api.js",
		"testdata/sample.txt", "mock_client.go", "fake_server.py",
		"app/user.spec.ts", "spec/models/user_spec.rb", "fixtures/replies.json",
	}
	for _, p := range yes {
		if !TestArtifactPath(p) {
			t.Errorf("expected test-artifact: %s", p)
		}
	}
	no := []string{
		"maze_explorer.py", "src/main.go", "solution.sh", "contest/answer.py",
		"latest.txt", "protest.go", // "test" only as a substring, not a segment/prefix
	}
	for _, p := range no {
		if TestArtifactPath(p) {
			t.Errorf("did not expect test-artifact: %s", p)
		}
	}
}
