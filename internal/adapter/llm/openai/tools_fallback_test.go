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
