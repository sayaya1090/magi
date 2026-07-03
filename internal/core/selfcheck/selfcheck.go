// Package selfcheck holds deterministic heuristics that catch an agent presenting
// fabricated work as a finished result. It is the shared core of two enforcement
// points that would otherwise drift apart: the loop's pre-finish / take-report
// gate (which scans files the agent wrote) and the report tool (which scans the
// text a subagent hands back). Keeping the marker list in one neutral package —
// depended on by both the app loop and the builtin tools — means the two never
// diverge into two half-maintained copies.
package selfcheck

import "strings"

// FabricationMarkers are phrases a model writes when it is admitting — inside the
// very artifact or report it is presenting as the solution — that the content is
// not the real result but a stand-in: a simulation, a placeholder, or "what a real
// run would have produced". They are multi-word and intent-revealing on purpose:
// bare "mock"/"stub"/"placeholder" appear legitimately in real code (a mocking
// library, an input placeholder), but "in a real implementation this would…" is a
// model narrating a fake. This keys off the MODEL'S OWN confession, not our guess
// at the task, which is what makes it a safe deterministic signal. Matched
// case-insensitively.
var FabricationMarkers = []string{
	"we can't actually",
	"we cannot actually",
	"can't actually run",
	"cannot actually run",
	"since we can't",
	"since we cannot",
	"for demonstration purposes",
	"in a real implementation",
	"in a real scenario",
	"in a real environment",
	"would be replaced with actual",
	"this would be replaced",
	"placeholder for actual",
	"this is a placeholder",
	"for the purpose of this task simulation",
}

// TestArtifactPath reports whether path is a test double's natural home — a test,
// mock, stub, or fixture file. Inside those, the marker phrases are legitimate
// engineering vocabulary ("this simulates the server", "in a real implementation
// this would hit the network"): a developer writing a test double is not an agent
// confessing a fake deliverable. The fabrication scan skips these paths so everyday
// coding never trips the gate; a bench task's deliverable never lives there.
func TestArtifactPath(path string) bool {
	p := strings.ToLower(strings.ReplaceAll(path, "\\", "/"))
	for _, seg := range strings.Split(p, "/") {
		switch seg {
		case "test", "tests", "testdata", "testing", "fixtures", "mocks", "stubs",
			"__tests__", "__mocks__", "spec", "specs":
			return true
		}
	}
	base := p[strings.LastIndexByte(p, '/')+1:]
	if strings.HasPrefix(base, "test_") || strings.HasPrefix(base, "mock_") ||
		strings.HasPrefix(base, "stub_") || strings.HasPrefix(base, "fake_") {
		return true
	}
	for _, infix := range []string{"_test.", ".test.", ".spec.", "_spec."} {
		if strings.Contains(base, infix) {
			return true
		}
	}
	return false
}

// FabricationMarker returns the first fabrication marker present in text and the
// trimmed, bounded line that contains it, or ("","") when text is clean. Both the
// match and the returned excerpt run on a lowercased copy: matching must be
// case-insensitive, and folding the excerpt too avoids byte-offset skew from
// non-ASCII case changes (the excerpt is a diagnostic, not a diff).
func FabricationMarker(text string) (marker, line string) {
	lc := strings.ToLower(text)
	for _, m := range FabricationMarkers {
		if i := strings.Index(lc, m); i >= 0 {
			start := strings.LastIndexByte(lc[:i], '\n') + 1
			end := len(lc)
			if nl := strings.IndexByte(lc[i:], '\n'); nl >= 0 {
				end = i + nl
			}
			excerpt := strings.TrimSpace(lc[start:end])
			if len(excerpt) > 160 {
				excerpt = excerpt[:160] + "…"
			}
			return m, excerpt
		}
	}
	return "", ""
}
