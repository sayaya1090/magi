package openai

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/sayaya1090/magi/internal/core/session"
)

// parseFallbackToolCall attempts to recover a tool call from assistant text for
// models that emit tool calls as plain content instead of native tool_calls
// (e.g. qwen2.5-coder via Ollama). It accepts a bare JSON object or one wrapped
// in a fenced code block, of the form:
//
//	{"name": "<tool>", "arguments": { ... }}
//
// The call is only returned if its name matches a tool that was offered (known),
// to avoid misclassifying ordinary JSON output as a tool call. (F-LLM-TOOLS-FALLBACK)
func parseFallbackToolCall(text string, known map[string]bool) (*session.ToolCall, bool) {
	s := stripFence(strings.TrimSpace(text))
	if !strings.HasPrefix(s, "{") {
		return nil, false
	}

	var probe struct {
		Name      string          `json:"name"`
		Tool      string          `json:"tool"` // alias some models use
		Arguments json.RawMessage `json:"arguments"`
		Params    json.RawMessage `json:"parameters"` // alias
	}
	if err := json.Unmarshal([]byte(s), &probe); err != nil {
		return nil, false
	}
	name := probe.Name
	if name == "" {
		name = probe.Tool
	}
	if name == "" || !known[name] {
		return nil, false
	}

	args := probe.Arguments
	if len(args) == 0 {
		args = probe.Params
	}
	args = normalizeArgs(args)

	return &session.ToolCall{Name: name, Args: args}, true
}

var (
	// Function opener, tolerating both <...> and [...] delimiters (qwen/Hermes
	// variants) and a missing closing tag: <function=bash>… or [function=bash]…
	xmlFuncRE = regexp.MustCompile(`(?s)[<\[]\s*function\s*=\s*([a-zA-Z0-9_.-]+)\s*[>\]]`)
	// Parameter OPENER only: <parameter=key> or [parameter=key]. The value is taken
	// separately (up to the closing tag), NOT by the regex — a value-capturing regex
	// would have to stop at some delimiter, and any choice truncates real argument
	// content (e.g. C's "#include <stdio.h>" cut at '<', or "arr[0]" cut at '[').
	xmlParamOpenRE = regexp.MustCompile(`[<\[]\s*parameter\s*=\s*([a-zA-Z0-9_.-]+)\s*[>\]]`)
	// A closing </parameter> or </function> tag (either delimiter style). Marks where
	// a parameter value ends — but a value may also end at the next parameter opener
	// or at end-of-text (missing close tag), handled in parseXMLToolCall.
	xmlCloseRE = regexp.MustCompile(`[<\[]\s*/\s*(?:parameter|function)\s*[>\]]`)
)

// parseXMLToolCall recovers a tool call from the Hermes/Qwen XML-ish format some
// models emit as content, tolerating delimiter variants and missing close tags:
//
//	<function=bash><parameter=command>ls</parameter></function>
//	[function=read][parameter=path]/x/y.go    (qwen3-coder variant)
//
// A parameter's value runs from its opening tag to whichever comes first: the next
// parameter opener, a closing </parameter>/</function> tag, or end-of-text. It is
// NOT cut at bare '<'/'[' inside the value, so code arguments survive intact.
// It searches anywhere in the text and validates the name against offered tools.
func parseXMLToolCall(text string, known map[string]bool) (*session.ToolCall, bool) {
	fm := xmlFuncRE.FindStringSubmatchIndex(text)
	if fm == nil {
		return nil, false
	}
	name := text[fm[2]:fm[3]]
	if !known[name] {
		return nil, false
	}
	body := text[fm[1]:]
	opens := xmlParamOpenRE.FindAllStringSubmatchIndex(body, -1)
	args := map[string]any{}
	for i, pm := range opens {
		key := body[pm[2]:pm[3]]
		valStart := pm[1] // just after this opening tag
		valEnd := len(body)
		if i+1 < len(opens) {
			valEnd = opens[i+1][0] // up to the next parameter opener
		}
		// …but stop earlier at a closing tag if one appears before that.
		if loc := xmlCloseRE.FindStringIndex(body[valStart:valEnd]); loc != nil {
			valEnd = valStart + loc[0]
		}
		args[key] = strings.TrimSpace(body[valStart:valEnd])
	}
	b, _ := json.Marshal(args)
	return &session.ToolCall{Name: name, Args: b}, true
}

// normalizeArgs ensures args is a JSON object. Some models emit arguments as a
// JSON-encoded string; unwrap that. Empty becomes "{}".
func normalizeArgs(args json.RawMessage) json.RawMessage {
	trimmed := strings.TrimSpace(string(args))
	if trimmed == "" || trimmed == "null" {
		return json.RawMessage("{}")
	}
	if strings.HasPrefix(trimmed, `"`) {
		var inner string
		if err := json.Unmarshal(args, &inner); err == nil {
			if strings.HasPrefix(strings.TrimSpace(inner), "{") {
				return json.RawMessage(inner)
			}
		}
	}
	return args
}

// stripFence removes a surrounding ```lang ... ``` fenced code block if present.
func stripFence(s string) string {
	if !strings.HasPrefix(s, "```") {
		return s
	}
	s = strings.TrimPrefix(s, "```")
	// Drop an optional language tag on the first line.
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	if i := strings.LastIndex(s, "```"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
