package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// A tool result's content is a JSON document, and the cap used to cut its BYTES — leaving an
// unterminated string literal, which the event marshaller refused and appendPart discarded. The
// call was then recorded with a null payload and the agent got no result at all.
//
// Measured (fix-ocaml-gc): `read /app/ocaml/runtime/major_gc.c` at 03:04:58 and
// `read /app/ocaml/runtime/shared_heap.c` at 03:06:18, the two files the task was about, both
// answered with `part.appended, data: null`. The agent moved on both times and never found the bug.
func TestAnOversizedResultStaysAResult(t *testing.T) {
	body := strings.Repeat("caml_major_collection_slice(intnat howmuch);\n", 2000)
	content, err := json.Marshal(body) // what a tool returns: json.Marshal of its text
	if err != nil {
		t.Fatal(err)
	}
	if len(content) <= toolResultCap {
		t.Fatalf("precondition: the payload must exceed the cap, got %d", len(content))
	}

	capped := capToolResult(content)
	if !json.Valid(capped) {
		t.Fatalf("a capped result must still be a JSON document; tail was %q", tail(capped, 80))
	}
	var got string
	if err := json.Unmarshal(capped, &got); err != nil {
		t.Fatalf("the capped payload must decode as the text it carries: %v", err)
	}
	if !strings.Contains(got, "caml_major_collection_slice") {
		t.Error("the truncated result must still carry the file's opening content")
	}
	if !strings.Contains(got, "output truncated") {
		t.Error("the truncation must be stated inside the payload the agent reads")
	}

	// The whole point: this survives the marshal that used to drop it.
	d, err := json.Marshal(event.PartAppendedData{MessageID: "m1", Role: session.RoleTool,
		Part: session.Part{Kind: session.PartToolResult, ToolResult: &session.ToolResult{
			CallID: "c1", Content: capped}}})
	if err != nil {
		t.Fatalf("the event carrying a capped result must marshal: %v", err)
	}
	if len(d) == 0 {
		t.Fatal("the event payload is empty — this is the null-data defect")
	}
}

// Structured output has no prefix that is still itself, so the cap says so rather than handing back
// something unparseable. Non-JSON content (a plugin returning raw bytes) keeps the byte-wise cut.
func TestCapKeepsEveryPayloadShapeReadable(t *testing.T) {
	big := make([]map[string]string, 4000)
	for i := range big {
		big[i] = map[string]string{"path": "internal/app/some/long/path/file.go", "kind": "match"}
	}
	structured, _ := json.Marshal(big)
	capped := capToolResult(structured)
	if !json.Valid(capped) {
		t.Fatal("a capped structured result must still be a JSON document")
	}
	var note string
	if err := json.Unmarshal(capped, &note); err != nil || !strings.Contains(note, "narrower query") {
		t.Errorf("it must say the output was omitted and what to do: %v %q", err, note)
	}

	raw := []byte(strings.Repeat("a", toolResultCap+5000)) // not a JSON document at all
	if got := capToolResult(raw); len(got) >= len(raw) || !strings.Contains(string(got), "output truncated") {
		t.Error("non-JSON content must keep the byte-wise cut with its marker")
	}
}

// A part that cannot be marshalled is recorded as the failure, never as nothing — and a tool result
// keeps its call id, so the agent can see WHICH call this answers.
func TestAnUnrecordablePartIsRecordedAsTheFailure(t *testing.T) {
	bad := session.Part{ID: "p1", Kind: session.PartToolResult,
		ToolResult: &session.ToolResult{CallID: "c9", Content: json.RawMessage(`{"broken`)}}
	if _, err := json.Marshal(event.PartAppendedData{Part: bad}); err == nil {
		t.Fatal("precondition: this part must be unmarshallable")
	}

	d := unrecordablePart("m1", session.RoleTool, bad, errFor("invalid character"))
	var got event.PartAppendedData
	if err := json.Unmarshal(d, &got); err != nil {
		t.Fatalf("the stand-in payload must be readable: %v", err)
	}
	if got.Part.ToolResult == nil || got.Part.ToolResult.CallID != "c9" {
		t.Fatalf("the stand-in must stay paired with its call, got %+v", got.Part)
	}
	if !got.Part.ToolResult.IsError {
		t.Error("a result that could not be recorded is not a success")
	}
	var text string
	_ = json.Unmarshal(got.Part.ToolResult.Content, &text)
	if !strings.Contains(text, "could not be recorded") || !strings.Contains(text, "narrower") {
		t.Errorf("it must say what happened and what to do instead: %q", text)
	}
}

// End to end through the seam the loop actually uses: an oversized read reaches the store as a
// readable result rather than a null event.
func TestOversizedResultReachesTheStore(t *testing.T) {
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow"})
	content, _ := json.Marshal(strings.Repeat("x", toolResultCap+4096))
	a.appendPart(context.Background(), sid, event.Actor{Kind: event.ActorAgent, ID: "a"}, "m1",
		session.RoleTool, session.Part{Kind: session.PartToolResult,
			ToolResult: &session.ToolResult{CallID: "c1", Content: capToolResult(content)}})

	for _, e := range mustRead(t, a, sid) {
		if e.Type != event.TypePartAppended {
			continue
		}
		if len(e.Data) == 0 || string(e.Data) == "null" {
			t.Fatal("a part was stored with a null payload — the agent would see no result")
		}
		var d event.PartAppendedData
		if json.Unmarshal(e.Data, &d) != nil || d.Part.ToolResult == nil {
			continue
		}
		if d.Part.ToolResult.CallID == "c1" {
			return // the result is there, paired with its call
		}
	}
	t.Fatal("the tool result never reached the store")
}

func tail(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[len(b)-n:])
}

type stringErr string

func (e stringErr) Error() string { return string(e) }

func errFor(s string) error { return stringErr(s) }
