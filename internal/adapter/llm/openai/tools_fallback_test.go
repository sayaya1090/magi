package openai

import (
	"encoding/json"
	"testing"
)

func TestParseFallbackToolCall(t *testing.T) {
	known := map[string]bool{"write": true, "read": true}

	cases := []struct {
		name     string
		text     string
		wantOK   bool
		wantName string
		wantPath string
	}{
		{
			// qwen2.5-coder via Ollama emits this as plain content.
			name:     "bare-json",
			text:     `{"name": "write", "arguments": {"path": "hello.txt", "content": "magi works"}}`,
			wantOK:   true,
			wantName: "write",
			wantPath: "hello.txt",
		},
		{
			name:     "fenced-json",
			text:     "```json\n{\"name\":\"read\",\"arguments\":{\"path\":\"x\"}}\n```",
			wantOK:   true,
			wantName: "read",
			wantPath: "x",
		},
		{
			name:     "tool-alias",
			text:     `{"tool":"read","parameters":{"path":"y"}}`,
			wantOK:   true,
			wantName: "read",
			wantPath: "y",
		},
		{
			name:   "plain-prose",
			text:   "I will now create the file for you.",
			wantOK: false,
		},
		{
			name:   "unknown-tool",
			text:   `{"name":"delete","arguments":{"path":"z"}}`,
			wantOK: false, // not in known set
		},
		{
			name:   "json-but-not-toolcall",
			text:   `{"foo":"bar"}`,
			wantOK: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseFallbackToolCall(tc.text, known)
			if ok != tc.wantOK {
				t.Fatalf("ok=%v want %v", ok, tc.wantOK)
			}
			if !tc.wantOK {
				return
			}
			if got.Name != tc.wantName {
				t.Errorf("name=%q want %q", got.Name, tc.wantName)
			}
			var args map[string]any
			if err := json.Unmarshal(got.Args, &args); err != nil {
				t.Fatalf("args not valid JSON: %s", got.Args)
			}
			if args["path"] != tc.wantPath {
				t.Errorf("path=%v want %q", args["path"], tc.wantPath)
			}
		})
	}
}

func TestParseXMLToolCall(t *testing.T) {
	known := map[string]bool{"bash": true}
	text := "I'll run it.\n<function=bash>\n<parameter=command>\nwc -w f.txt\n</parameter>\n</function>"
	tc, ok := parseXMLToolCall(text, known)
	if !ok || tc.Name != "bash" {
		t.Fatalf("xml parse: ok=%v name=%v", ok, tc)
	}
	var args map[string]any
	_ = json.Unmarshal(tc.Args, &args)
	if args["command"] != "wc -w f.txt" {
		t.Errorf("command=%v want 'wc -w f.txt'", args["command"])
	}
}

// A parameter value containing '<' or '[' (C includes, generics, array indexing)
// must NOT be truncated at the first such char — the bug that made "write main.c"
// save only "#include" (8 bytes), cut at "<stdio.h>".
func TestParseXMLToolCallValueWithAngleBrackets(t *testing.T) {
	known := map[string]bool{"write": true}
	content := "#include <stdio.h>\n\nint main() {\n    printf(\"Hello, World!\\n\");\n    return 0;\n}"
	text := "<function=write><parameter=path>main.c</parameter>" +
		"<parameter=content>" + content + "</parameter></function>"
	tc, ok := parseXMLToolCall(text, known)
	if !ok {
		t.Fatal("expected a tool call")
	}
	var args map[string]any
	if err := json.Unmarshal(tc.Args, &args); err != nil {
		t.Fatal(err)
	}
	if args["path"] != "main.c" {
		t.Errorf("path=%v want main.c", args["path"])
	}
	if args["content"] != content {
		t.Errorf("content was truncated:\n got=%q\nwant=%q", args["content"], content)
	}
}

// The qwen3-coder [bracket] variant with a value that itself contains '[' must also
// survive, and a missing closing tag must still capture to the next opener / end.
func TestParseXMLToolCallBracketVariantAndArrays(t *testing.T) {
	known := map[string]bool{"write": true}
	content := "xs := []int{1, 2, 3}\nv := xs[0]"
	text := "[function=write][parameter=path]a.go[parameter=content]" + content
	tc, ok := parseXMLToolCall(text, known)
	if !ok {
		t.Fatal("expected a tool call")
	}
	var args map[string]any
	_ = json.Unmarshal(tc.Args, &args)
	if args["path"] != "a.go" {
		t.Errorf("path=%v want a.go", args["path"])
	}
	if args["content"] != content {
		t.Errorf("content truncated:\n got=%q\nwant=%q", args["content"], content)
	}
}

// A model without native tool calling expresses the action AS this reply, so a parse failure does
// not degrade the call — it erases it, and the text is shown as prose with no sign anything was
// lost. The recovery must therefore survive what model output normally carries.
func TestParseFallbackToolCallSurvivesModelJSONDefects(t *testing.T) {
	known := map[string]bool{"write": true, "bash": true}
	cases := []struct{ name, text, wantName string }{
		{"clean", `{"name":"bash","arguments":{"command":"ls"}}`, "bash"},
		{"raw newline in an argument",
			"{\"name\":\"write\",\"arguments\":{\"content\":\"line1\nline2\"}}", "write"},
		{"trailing comma", `{"name":"bash","arguments":{"command":"ls"},}`, "bash"},
		{"fenced", "```json\n{\"name\":\"bash\",\"arguments\":{\"command\":\"ls\"}}\n```", "bash"},
	}
	for _, c := range cases {
		tc, ok := parseFallbackToolCall(c.text, known)
		if !ok || tc.Name != c.wantName {
			t.Errorf("%s: ok=%v name=%q, want %q", c.name, ok, func() string {
				if tc == nil {
					return ""
				}
				return tc.Name
			}(), c.wantName)
		}
	}
	// An unknown tool or a non-object reply is still not a call.
	for _, bad := range []string{`{"name":"nope","arguments":{}}`, "just prose", `{"arguments":{}}`} {
		if _, ok := parseFallbackToolCall(bad, known); ok {
			t.Errorf("parseFallbackToolCall(%q) must not produce a call", bad)
		}
	}
}

// repairArgs fixes the argument payload once, where a call is finalized, so the forty tools that
// unmarshal their own arguments do not each need the same tolerance.
func TestRepairArgs(t *testing.T) {
	clean := json.RawMessage(`{"command":"ls"}`)
	if got := repairArgs(clean); string(got) != string(clean) {
		t.Errorf("valid args must be untouched, got %s", got)
	}
	fixed := repairArgs(json.RawMessage("{\"content\":\"a\nb\"}"))
	var m map[string]string
	if json.Unmarshal(fixed, &m) != nil || m["content"] != "a\nb" {
		t.Errorf("a raw newline must be repaired into the same text, got %s", fixed)
	}
	bad := json.RawMessage(`{"a":,}`)
	if got := repairArgs(bad); string(got) != string(bad) {
		t.Errorf("an irreparable payload must be left as-is so the tool reports the real error, got %s", got)
	}
}
