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
