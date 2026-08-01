package app

import (
	"encoding/json"
	"os"
	"strings"
)

// Every tool call carries a short `intent`: why THIS call is being made.
//
// magi already records what was called and with what bytes. What it has never had is what the call
// was FOR, and several of its own measurements are weaker for the lack. The repeat guard decides
// "the same call again" by fingerprinting arguments, so a rebuild re-issued twelve times byte for
// byte counts as twelve of one thing while the same work re-issued under a shuffled command counts
// as twelve different things (both observed on 2026-08-01). And the evidence the council reads is a
// list of commands, which is the least legible form of what an agent spent its turn doing.
//
// The field is declared centrally rather than tool by tool: every tool gets it, including the ones
// magi does not own (plugin and MCP tools arrive with their own schemas). That is one seam to
// change instead of forty, and it cannot drift out of sync between the schema the model is shown
// and the schema magi validates against — they are the same function's output.
//
// It is REQUIRED in the schema and unenforced in the code. Required is what gets a model to fill
// it in; enforcing would turn a missing label into a refused call, and a call magi can run is worth
// more than a call it can describe.
const toolIntentKey = "intent"

const toolIntentDesc = "Why this call is being made — a short phrase, not a restatement of the arguments."

// toolIntentEnabled gates the field so a wave can be run with and without it. Default on.
func toolIntentEnabled() bool {
	return !strings.EqualFold(strings.TrimSpace(os.Getenv("MAGI_TOOL_INTENT")), "off")
}

// schemaWithIntent returns the tool's parameter schema with `intent` declared and required.
//
// Object schemas only, and only ones that already declare properties: a tool that takes no
// arguments, or whose schema is a shape this does not understand, is returned untouched rather than
// rewritten into something its own parser may not accept. A schema that already declares `intent`
// keeps its own — the tool means something by it.
func schemaWithIntent(raw json.RawMessage) json.RawMessage {
	if !toolIntentEnabled() || len(raw) == 0 {
		return raw
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return raw
	}
	if t, ok := m["type"]; !ok || string(t) != `"object"` {
		return raw
	}
	var props map[string]json.RawMessage
	if p, ok := m["properties"]; !ok || json.Unmarshal(p, &props) != nil || len(props) == 0 {
		return raw
	}
	if _, taken := props[toolIntentKey]; taken {
		return raw
	}
	desc, err := json.Marshal(map[string]string{"type": "string", "description": toolIntentDesc})
	if err != nil {
		return raw
	}
	props[toolIntentKey] = desc
	pb, err := json.Marshal(props)
	if err != nil {
		return raw
	}
	m["properties"] = pb

	var req []string
	if r, ok := m["required"]; ok && json.Unmarshal(r, &req) != nil {
		return raw // a required list in a shape we don't understand — leave the whole schema alone
	}
	req = append(req, toolIntentKey)
	rb, err := json.Marshal(req)
	if err != nil {
		return raw
	}
	m["required"] = rb

	out, err := json.Marshal(m)
	if err != nil {
		return raw
	}
	return out
}

// stripToolIntent removes the intent from a call's arguments.
//
// Every comparison magi makes between two calls has to run on this, not on the raw args. The intent
// is prose the model writes fresh each time, so leaving it in the loop guard's fingerprint would
// make two identical calls look like two different ones — the guard would notice LESS than it did
// before the field existed. The same holds for the mutation signature, where an identical write
// under new wording would read as a real change and reset the no-progress counters.
//
// Returns the input unchanged when there is nothing to strip, so the common path allocates nothing.
func stripToolIntent(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return raw
	}
	if _, ok := m[toolIntentKey]; !ok {
		return raw
	}
	delete(m, toolIntentKey)
	out, err := json.Marshal(m)
	if err != nil {
		return raw
	}
	return out
}
