package openai

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/jsonx"
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

	type callProbe struct {
		Name      string          `json:"name"`
		Tool      string          `json:"tool"` // alias some models use
		Arguments json.RawMessage `json:"arguments"`
		Params    json.RawMessage `json:"parameters"` // alias
	}
	// Scan every balanced object and apply the shared repairs: for a model without native tool
	// calling this reply IS the action, so failing to parse it does not degrade the call — it
	// erases it, and the text is then shown as ordinary prose with no sign anything was lost.
	var probe callProbe
	parsed := false
	for _, js := range jsonx.Objects(s) {
		var p callProbe
		if jsonx.Unmarshal(js, &p) && (p.Name != "" || p.Tool != "") {
			probe, parsed = p, true
			break
		}
	}
	if !parsed {
		fmt.Fprintf(os.Stderr, "magi: a reply that looks like a tool call did not parse (%d bytes); the loop will ask for a repair\n", len(s))
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

// fallbackCallName is the name a call-shaped reply used, whether or not it is a tool — "" when it
// named nothing. It is what the repair request quotes back when the name is not a tool: a model
// that wrote {"name":"excel_add_comment"} (the name of a saved SKILL, 2026-09-07) is told that
// name is not callable, which is a different correction from "you named nothing".
func fallbackCallName(text string) string {
	s := stripFence(strings.TrimSpace(text))
	if m := xmlFuncRE.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	type probe struct {
		Name string `json:"name"`
		Tool string `json:"tool"`
	}
	for _, js := range jsonx.Objects(s) {
		var p probe
		if jsonx.Unmarshal(js, &p) {
			if p.Name != "" {
				return p.Name
			}
			if p.Tool != "" {
				return p.Tool
			}
		}
	}
	return ""
}

// looksLikeToolCall reports whether assistant text has the SHAPE of a tool call even when it
// cannot be read as one: a single JSON object (optionally fenced) or a <function=…>/[function=…]
// opener. Ordinary prose, arrays and numbers are not. It is what turns "treat it as text" into
// "ask for a repair" (port.ProviderEvent.MalformedCall) — a reply that is one object is an action
// the model failed to phrase, not an answer for the person.
func looksLikeToolCall(text string) bool {
	s := stripFence(strings.TrimSpace(text))
	// The XML form counts only when the reply STARTS with it — prose that mentions the syntax, or
	// code that contains it, is an answer (review 2026-09-07).
	if loc := xmlFuncRE.FindStringIndex(s); loc != nil && loc[0] == 0 {
		return true
	}
	return strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}")
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
//
// The unwrapped payload gets the same repairs the native path applies at finish(): it is a SECOND,
// independently authored JSON document that the outer parse never validated, so a defect inside it
// (an unescaped quote in a `command`, a raw newline in `content`) survives an outer object that
// parsed perfectly — and here, where the reply IS the action, that erases the call rather than
// degrading it.
func normalizeArgs(args json.RawMessage) json.RawMessage {
	trimmed := strings.TrimSpace(string(args))
	if trimmed == "" || trimmed == "null" {
		return json.RawMessage("{}")
	}
	if strings.HasPrefix(trimmed, `"`) {
		var inner string
		if err := json.Unmarshal(args, &inner); err == nil {
			if strings.HasPrefix(strings.TrimSpace(inner), "{") {
				return repairArgs(json.RawMessage(inner))
			}
		}
	}
	// An object the model wrote DIRECTLY gets the repair too. It did not before, and the same
	// defect therefore had two fates on the same path: `"{\"command\":\"echo \"hi\"\"}"` came out
	// as usable JSON because unwrapping ran it through repairArgs, while the identical
	// `{"command":"echo "hi""}` came out untouched and unparseable — and here, where the reply IS
	// the action, unparseable erases the call rather than degrading it.
	//
	// The native path repairs every call's arguments at finish(). This is the path taken when the
	// model was too weak to emit a native tool call at all, which is not the population to give
	// the weaker treatment.
	//
	// Only an object is repaired. An array, a bare word or a quoted non-object cannot be turned
	// into arguments without inventing them, so they are handed on as they are and rejected by
	// whoever asked for a shape.
	if strings.HasPrefix(trimmed, "{") {
		return repairArgs(args)
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
