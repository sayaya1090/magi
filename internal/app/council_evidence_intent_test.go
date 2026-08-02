package app

import (
	"encoding/json"
	"strings"
	"testing"
)

// A write's evidence line is "tool write [ok] /app/extract.js: wrote 3893 bytes" — a fact with no
// subject, because evidenceArgs shows the path and the content is not in the block. The intent is
// the only place the turn says what the file was for, so it belongs on that line.
//
// Labelled as the agent's own words: a member weighing whether the work matches the task must not
// read a claim of purpose as a finding about the artifact.
func TestTheEvidenceLineCarriesTheStatedPurpose(t *testing.T) {
	b := toolCallBrief{
		name:   "write",
		args:   evidenceArgs("write", json.RawMessage(`{"path":"/app/extract.js","content":"…"}`)),
		intent: intentArg(json.RawMessage(`{"path":"/app/extract.js","intent":"Rewrite the extractor to map file offsets through the program headers"}`)),
	}
	line := evidenceLine(b, "ok", "wrote 3893 bytes to /app/extract.js")
	for _, want := range []string{"/app/extract.js", "agent's stated purpose", "program headers", "wrote 3893 bytes"} {
		if !strings.Contains(line, want) {
			t.Errorf("%q missing from:\n%s", want, line)
		}
	}
	// It arrives on 60% of calls; the line has to read correctly without it.
	bare := toolCallBrief{name: "bash", args: "go build ./..."}
	line = evidenceLine(bare, "ok", "")
	if strings.Contains(line, "stated purpose") {
		t.Errorf("a call with no intent must not grow an empty clause:\n%s", line)
	}
	if !strings.Contains(line, "go build") {
		t.Errorf("the command is still what identifies it:\n%s", line)
	}
	// And it is clipped: a paragraph in the intent must not crowd out the result.
	long := toolCallBrief{name: "bash", args: "x", intent: strings.Repeat("purpose ", 60)}
	if n := len(evidenceLine(long, "ok", "out")); n > 400 {
		t.Errorf("the line grew to %d chars on a long intent", n)
	}
}
