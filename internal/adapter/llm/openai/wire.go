package openai

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/envflag"
	"github.com/sayaya1090/magi/internal/port"
)

// ---- request wire format (OpenAI Chat Completions) ----

type chatRequest struct {
	Model         string         `json:"model"`
	Stream        bool           `json:"stream"`
	Messages      []wireMessage  `json:"messages"`
	Tools         []wireTool     `json:"tools,omitempty"`
	StreamOptions *streamOptions `json:"stream_options,omitempty"`
	// ReasoningEffort controls a "thinking" model's reasoning budget (OpenAI-compat
	// field; Ollama honors "none" to disable thinking). Empty = omit → provider
	// default, so non-thinking models are unaffected.
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
	// MaxTokens caps the output tokens per response ([limits] max_output_tokens); 0 =
	// omit → provider default.
	MaxTokens int `json:"max_tokens,omitempty"`
	// Temperature/TopP/TopK are the sampling settings: a per-request pin from
	// port.ChatRequest.Params, else the configured default ([sampling] in config.toml).
	//
	// All three are POINTERS because 0 is a value callers genuinely send — temperature 0 is
	// the reproducibility pin a structured-JSON call makes — and with a plain numeric the
	// `omitempty` that keeps "unset" out of the body would delete exactly that. nil = omit →
	// the provider's own default stands.
	Temperature *float64 `json:"temperature,omitempty"`
	TopP        *float64 `json:"top_p,omitempty"`
	// TopK is NOT part of the OpenAI Chat Completions schema; it is an extension some
	// backends accept and others reject. It is therefore only ever sent when explicitly
	// configured, never by default. Measured against Ollama's /v1 endpoint: top_k is
	// accepted (HTTP 200) but IGNORED — top_k=1 at temperature 1.6 still sampled freely,
	// while the same request to the native /api/chat came back greedy. Configure it only
	// against a backend known to honor it.
	TopK *int `json:"top_k,omitempty"`
}

// Sampling carries the configured sampling defaults ([sampling] in config.toml, or the
// MAGI_TEMPERATURE/MAGI_TOP_P/MAGI_TOP_K env overrides). A nil field is "not configured": the
// field is omitted from the request and the provider's default stands. A caller's per-request
// pin in port.ChatRequest.Params outranks all of these — the council pins temperature 0 on its
// member polls because those calls distill structured documents, and no config default should
// quietly un-pin them.
type Sampling struct {
	Temperature *float64
	TopP        *float64
	TopK        *int
	// ReasoningEffort is not a sampling parameter on the wire — it is its own top-level field —
	// but it comes from the same [sampling] table and is set the same way, so it travels with
	// them rather than growing a second option for one string.
	ReasoningEffort string
}

// streamOptions asks the server to emit a final usage chunk while streaming.
type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type wireMessage struct {
	Role string `json:"role"`
	// Content is always emitted (no omitempty): an assistant message that carries
	// only tool_calls still needs an explicit "" — some OpenAI-compatible gateways
	// (e.g. LiteLLM → certain providers) reject a missing/null content field. It is
	// usually a string, but becomes a []textBlock when a cache_control breakpoint
	// is attached (prompt caching).
	Content    any            `json:"content"`
	ToolCalls  []wireToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	// Reasoning hands an assistant message's own thinking back to the model. A harmony-family
	// model (gpt-oss) plans in an analysis channel it expects to SEE again while the turn is
	// still open — measured on Ollama 0.32.6: in a tool-continuation request this field is
	// rendered into the prompt (prompt_tokens 139 → 747 with a filler), and after a newer user
	// message the template itself drops it, which is the harmony contract. Providers that know
	// no such field ignore it. Sent only when the resend flag is on — see convertMessages.
	Reasoning string `json:"reasoning,omitempty"`
}

// cacheControl marks a prompt-cache breakpoint (Anthropic, forwarded by LiteLLM).
type cacheControl struct {
	Type string `json:"type"` // "ephemeral"
}

// textBlock is an OpenAI content part; cache_control caches the prefix up to here.
type textBlock struct {
	Type         string        `json:"type"` // "text"
	Text         string        `json:"text"`
	CacheControl *cacheControl `json:"cache_control,omitempty"`
}

func ephemeral() *cacheControl { return &cacheControl{Type: "ephemeral"} }

type wireTool struct {
	Type         string        `json:"type"` // "function"
	CacheControl *cacheControl `json:"cache_control,omitempty"`
	Function     wireFunction  `json:"function"`
}

type wireFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type wireToolCall struct {
	Index    int          `json:"index"`
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function wireFuncCall `json:"function"`
}

type wireFuncCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ---- streaming response wire format ----

type streamChunk struct {
	Choices []streamChoice `json:"choices"`
	Usage   *wireUsage     `json:"usage,omitempty"`
}

type streamChoice struct {
	Delta        streamDelta `json:"delta"`
	FinishReason *string     `json:"finish_reason"`
}

type streamDelta struct {
	Role      string         `json:"role,omitempty"`
	Content   string         `json:"content,omitempty"`
	ToolCalls []wireToolCall `json:"tool_calls,omitempty"`
	// Reasoning ("thinking") tokens — providers use different field names.
	Reasoning        string `json:"reasoning,omitempty"`
	ReasoningContent string `json:"reasoning_content,omitempty"`
}

// reasoningText returns whichever reasoning field the provider populated.
func (d streamDelta) reasoningText() string {
	if d.Reasoning != "" {
		return d.Reasoning
	}
	return d.ReasoningContent
}

// wireUsage reads a usage block under EITHER spelling.
//
// `prompt_tokens`/`completion_tokens` is the chat-completions wording; `input_tokens`/
// `output_tokens` is what the Responses API, Anthropic's native API, and gateways that pass either
// through use; `in`/`out` is what a thin proxy emits. A backend on any spelling but the first parsed
// as ZERO here — no error, no warning, just a run whose token totals were silently empty and a cost
// of $0.00 next to real spend.
//
// prompt_tokens_details.cached_tokens IS read, since 2026-08-07: something displays it now. The
// console shows what share of a prompt the backend served from its cache, which is the only honest
// answer to "is the prefix cache working" — and the alternative, a number derived from timings,
// measures the machine's load as much as the cache.
//
// Reported is separate from the count, because zero and silence are different facts. A backend that
// sends `cached_tokens: 0` is saying the cache missed; one that sends no details block at all is
// saying nothing, and drawing that as 0% would report a working cache as broken. Measured on the
// default local backend (Ollama /v1, 2026-08-07): it sends prompt/completion/total and no details
// block, so on that backend this stays silent — which is why the distinction had to exist before
// the display did.
//
// completion_tokens_details.reasoning_tokens is still not read: nothing shows it, and pricing it
// differently needs a per-model rate the registry does not carry.
type wireUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	InputTokens      int `json:"input_tokens"`
	OutputTokens     int `json:"output_tokens"`
	// The bare spelling, as a thin proxy or a gateway that normalises to magi's own event shape
	// emits it. Cheap to accept and indistinguishable in meaning from the other two.
	BareIn  int `json:"in"`
	BareOut int `json:"out"`
	Details *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details,omitempty"`
}

// Cached is how much of the prompt the backend served from its cache, and whether it said at all.
func (u wireUsage) Cached() (int, bool) {
	if u.Details == nil {
		return 0, false
	}
	return u.Details.CachedTokens, true
}

// In and Out prefer whichever spelling actually carried a number, so a payload that sends both (or
// sends one as an explicit zero) reads the same either way.
func (u wireUsage) In() int  { return firstNonZero(u.PromptTokens, u.InputTokens, u.BareIn) }
func (u wireUsage) Out() int { return firstNonZero(u.CompletionTokens, u.OutputTokens, u.BareOut) }

// firstNonZero picks the spelling that actually carried a number. Order is preference, not
// precedence over truth: a payload that sends one spelling as an explicit 0 and another with the
// real count reads the real count, which is the only useful reading of a usage block.
func firstNonZero(vs ...int) int {
	for _, v := range vs {
		if v > 0 {
			return v
		}
	}
	return 0
}

// buildRequest converts a port.ChatRequest into the wire request body. When
// cache is set, it attaches cache_control breakpoints to the (large, stable)
// system prompt and tool list so an Anthropic model behind LiteLLM caches that
// prefix instead of re-billing it every turn.
func buildRequest(r port.ChatRequest, stream, cache bool, reasoningEffort string, maxTokens int, samp Sampling) chatRequest {
	out := chatRequest{Model: r.Model, Stream: stream, ReasoningEffort: reasoningEffort, MaxTokens: maxTokens}
	out.Temperature, out.TopP, out.TopK = samp.Temperature, samp.TopP, samp.TopK
	// A per-request pin overrides the configured default, field by field: a call that pins only
	// the temperature keeps the configured top_p rather than reverting it to the provider's.
	if v := floatParam(r.Params, "temperature"); v != nil {
		out.Temperature = v
	}
	if v := floatParam(r.Params, "top_p"); v != nil {
		out.TopP = v
	}
	if v := intParam(r.Params, "top_k"); v != nil {
		out.TopK = v
	}
	if stream {
		out.StreamOptions = &streamOptions{IncludeUsage: true}
	}

	if r.System != "" {
		sys := wireMessage{Role: "system", Content: r.System}
		if cache {
			sys.Content = []textBlock{{Type: "text", Text: r.System, CacheControl: ephemeral()}}
		}
		out.Messages = append(out.Messages, sys)
	}
	// Pictures ride only to a model that can read them — see canSeeImages, the first reader the
	// Vision flag has ever had.
	out.Messages = append(out.Messages, convertMessages(r.Messages, canSeeImages(r.Model))...)
	out.Messages = normalizeSystemPlacement(out.Messages)

	for _, t := range r.Tools {
		out.Tools = append(out.Tools, wireTool{
			Type: "function",
			Function: wireFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Schema,
			},
		})
	}
	// Cache the whole tool list by marking the last tool (Anthropic caches the
	// prefix up to a cache_control point).
	if cache && len(out.Tools) > 0 {
		out.Tools[len(out.Tools)-1].CacheControl = ephemeral()
	}
	return out
}

// floatParam reads one of the caller's sampling pins out of port.ChatRequest.Params.
//
// port documents Params as carrying sampling overrides, and the council sets
// {"temperature": 0.0} on every member poll — but nothing here ever read the map, so that pin
// was dropped on the floor and every deliberation ran at the provider's default sampling. The
// cost is invisible in a single run and severe across runs: the same plan, audited twice, yields
// a different set of executable checks, and one draw in three yields NONE (measured on the local
// backend: two temperature-0 fills authored the same 3 checks, while three default-temperature
// fills authored 3, 0, and a reply with a raw control character in it).
//
// Numeric JSON shapes are all accepted because a param map can arrive from config as well as
// from Go code. An unusable value is ignored rather than defaulted, so a typo cannot silently
// re-pin the model to something the caller never asked for.
func floatParam(params map[string]any, key string) *float64 {
	v, ok := params[key]
	if !ok {
		return nil
	}
	var f float64
	switch n := v.(type) {
	case float64:
		f = n
	case float32:
		f = float64(n)
	case int:
		f = float64(n)
	case int64:
		f = float64(n)
	case json.Number:
		parsed, err := n.Float64()
		if err != nil {
			return nil
		}
		f = parsed
	default:
		return nil
	}
	if f < 0 { // negative is not a sampling setting any backend accepts
		return nil
	}
	return &f
}

// intParam is floatParam for top_k, which is a count rather than a probability. A fractional
// value is rejected rather than truncated: 0.5 is not somebody asking for "top 0", it is a
// mistake, and truncating it would silently forbid every token.
func intParam(params map[string]any, key string) *int {
	f := floatParam(params, key)
	if f == nil {
		return nil
	}
	n := int(*f)
	if float64(n) != *f {
		return nil
	}
	return &n
}

// repairToolOrdering makes the message sequence valid for strict OpenAI-compatible
// backends (LiteLLM, the OpenAI API) where every `tool` message MUST immediately
// follow the assistant message that declared its tool call. Lenient backends
// (Ollama) accept loose ordering, but strict ones reject with "Message has tool
// role, but there was no previous assistant message with a tool call".
//
// Two corruptions are repaired, both of which arise in normal operation:
//  1. A message injected between an assistant tool-call and its result (an async
//     subagent result, a hook nudge, a user steer) → the tool results are pulled
//     up to sit directly after their assistant, and the injected messages follow.
//  2. An orphaned tool result whose assistant tool-call was dropped by compaction
//     (the boundary split the pair) or never existed → demoted to a user message
//     so its content is preserved without breaking the wire contract.
//
// Additionally, an assistant tool-call with no result anywhere (the opposite
// compaction split) gets a synthetic placeholder result so strict backends don't
// reject the dangling call.
func repairToolOrdering(msgs []session.Message) []session.Message {
	// Pass 1: index every tool result by call id, and the set of call ids that
	// some assistant actually declared.
	resultByID := map[string]session.Part{}
	declared := map[string]bool{}
	hasTools := false
	for _, m := range msgs {
		for _, p := range m.Parts {
			if p.Kind == session.PartToolResult && p.ToolResult != nil {
				if _, ok := resultByID[p.ToolResult.CallID]; !ok {
					resultByID[p.ToolResult.CallID] = p
				}
				hasTools = true
			}
			if p.Kind == session.PartToolCall && p.ToolCall != nil {
				declared[p.ToolCall.CallID] = true
				hasTools = true
			}
		}
	}
	if !hasTools {
		return msgs // nothing to repair — the common chat-only path
	}

	emitted := map[string]bool{}
	out := make([]session.Message, 0, len(msgs))
	for _, m := range msgs {
		switch m.Role {
		case session.RoleAssistant:
			out = append(out, m)
			// Immediately follow with this assistant's tool results, in call order.
			for _, p := range m.Parts {
				if p.Kind != session.PartToolCall || p.ToolCall == nil {
					continue
				}
				id := p.ToolCall.CallID
				if res, ok := resultByID[id]; ok {
					out = append(out, session.Message{Role: session.RoleTool, Parts: []session.Part{res}})
				} else {
					out = append(out, session.Message{Role: session.RoleTool, Parts: []session.Part{{
						Kind:       session.PartToolResult,
						ToolResult: &session.ToolResult{CallID: id, Content: json.RawMessage(`"(no result returned for this tool call)"`)},
					}}})
				}
				emitted[id] = true
			}
		case session.RoleTool:
			// Tool results are emitted next to their assistant above. Keep only the
			// parts that can't be (orphans), demoted to a user message.
			var orphans []session.Part
			for _, p := range m.Parts {
				if p.Kind == session.PartToolResult && p.ToolResult != nil && !declared[p.ToolResult.CallID] {
					orphans = append(orphans, p)
				}
			}
			for _, p := range orphans {
				out = append(out, session.Message{Role: session.RoleUser, Parts: []session.Part{{
					Kind: session.PartText,
					Text: "[tool result] " + toolResultContent(p.ToolResult.Content),
				}}})
			}
		default:
			out = append(out, m)
		}
	}
	return out
}

// wireRoleDiag renders a compact role-sequence summary of a request's messages for
// diagnostics — ROLES ONLY, never content (a compacted history reaching this layer can be
// ~100 MB; dumping it would be ruinous). It surfaces exactly what a "system message must be
// at the beginning" 400 needs to be root-caused from a run log: how many system messages the
// wire request carries and at which indices (after normalizeSystemPlacement), plus the head
// of the role run. Appended to the error text on a 400/422 so it lands in the run transcript.
func wireRoleDiag(msgs []wireMessage) string {
	var sysIdx []int
	roles := make([]string, 0, len(msgs))
	for i, m := range msgs {
		if m.Role == "system" {
			sysIdx = append(sysIdx, i)
		}
		if i < 20 {
			roles = append(roles, m.Role)
		}
	}
	head := strings.Join(roles, ",")
	if len(msgs) > 20 {
		head += ",…"
	}
	return fmt.Sprintf("[wire-diag: %d msgs, systemCount=%d at idx %v, roles=%s]", len(msgs), len(sysIdx), sysIdx, head)
}

// normalizeSystemPlacement enforces the strictest common template contract: at most
// ONE system message, at position 0. Strict OpenAI-compatible chat templates
// (Mistral-family, some vLLM/llama.cpp templates, no-system-role model families)
// return HTTP 400 on a second or mid-conversation system message — which magi can
// legitimately produce (a compaction summary is reconstructed as a system-role
// message right after the main system prompt). Leading system messages are merged
// into one; any later system message is demoted to a user message with a
// "[system note]" prefix. String-content merge only — a cache_control textBlock
// system stays intact as the head (the merge appends the extra text as plain string
// is impossible there, so the extra becomes a demoted note instead).
func normalizeSystemPlacement(msgs []wireMessage) []wireMessage {
	out := make([]wireMessage, 0, len(msgs))
	headDone := false
	for i, m := range msgs {
		if m.Role != "system" {
			out = append(out, m)
			if len(out) > 0 {
				headDone = true
			}
			continue
		}
		// System message: first one at the very head passes through.
		if i == 0 {
			out = append(out, m)
			continue
		}
		if !headDone && len(out) > 0 && out[0].Role == "system" {
			// Still in the leading system run — merge string contents.
			if hs, ok := out[0].Content.(string); ok {
				if ms, ok2 := m.Content.(string); ok2 {
					out[0].Content = hs + "\n\n" + ms
					continue
				}
			}
		}
		// Mid-conversation (or unmergeable) system → demote to a prefixed user message.
		txt := ""
		if s, ok := m.Content.(string); ok {
			txt = s
		} else if b, err := json.Marshal(m.Content); err == nil {
			txt = string(b)
		}
		out = append(out, wireMessage{Role: "user", Content: "[system note]\n" + txt})
	}
	return out
}

// convertMessages maps domain messages (with parts) to OpenAI wire messages.
func convertMessages(msgs []session.Message, withImages bool) []wireMessage {
	// Which tool results get to carry their pictures, decided BACKWARDS from the end of the
	// conversation: the newest are the ones being talked about. Without this the first render of a
	// long session would keep its place in every request while the one just made was dropped.
	showImages := map[string]bool{}
	if withImages {
		showImages = recentImages(msgs)
	}
	msgs = repairToolOrdering(msgs)
	// The resend window: assistant reasoning travels back only for messages AFTER the last user
	// message — the open turn's own tool-calling continuation. That is where the harmony contract
	// wants it (and where Ollama's template renders it); older reasoning is dropped by the
	// template anyway, so sending it would be bytes on the wire buying nothing.
	lastUser := -1
	if resendReasoning() {
		for i, m := range msgs {
			if m.Role == session.RoleUser {
				lastUser = i
			}
		}
	}
	var out []wireMessage
	for i, m := range msgs {
		switch m.Role {
		case session.RoleUser, session.RoleSystem:
			out = append(out, wireMessage{Role: string(m.Role), Content: joinText(m.Parts)})
		case session.RoleAssistant:
			wm := wireMessage{Role: "assistant", Content: joinText(m.Parts)}
			if resendReasoning() && i > lastUser {
				wm.Reasoning = joinReasoning(m.Parts)
			}
			for _, p := range m.Parts {
				if p.Kind == session.PartToolCall && p.ToolCall != nil {
					wm.ToolCalls = append(wm.ToolCalls, wireToolCall{
						ID:       p.ToolCall.CallID,
						Type:     "function",
						Function: wireFuncCall{Name: p.ToolCall.Name, Arguments: string(p.ToolCall.Args)},
					})
				}
			}
			out = append(out, wm)
		case session.RoleTool:
			// Each tool result becomes its own tool message.
			for _, p := range m.Parts {
				if p.Kind == session.PartToolResult && p.ToolResult != nil {
					out = append(out, wireMessage{
						Role:       "tool",
						ToolCallID: p.ToolResult.CallID,
						Content:    toolResultContent(p.ToolResult.Content),
					})
					// And its pictures after it, as their own user message: the API gives a tool
					// result nowhere to put one (role "tool" takes a string). The result's text
					// names them either way, so a model that cannot see them still knows they
					// exist — and one that can is shown them next to the call they came from.
					if !showImages[p.ToolResult.CallID] {
						continue
					}
					blocks, left, _ := imagesFor(p.ToolResult.Images, imageBudget, imagesPerReply)
					if len(blocks) > 0 {
						out = append(out, imageMessage(p.ToolResult.CallID, blocks, left))
					}
				}
			}
		}
	}
	return out
}

func joinText(parts []session.Part) string {
	var b []byte
	for _, p := range parts {
		if p.Kind == session.PartText {
			if len(b) > 0 {
				b = append(b, '\n')
			}
			b = append(b, p.Text...)
		}
	}
	return string(b)
}

// joinReasoning is joinText for the reasoning parts — what a thinking model streamed as analysis,
// persisted by the loop, and (behind the resend flag) handed back on the wire.
func joinReasoning(parts []session.Part) string {
	var b []byte
	for _, p := range parts {
		if p.Kind == session.PartReasoning {
			if len(b) > 0 {
				b = append(b, '\n')
			}
			b = append(b, p.Text...)
		}
	}
	return string(b)
}

// resendReasoning gates the whole H1 lever. Default ON since the paired pilot (2026-08-16,
// gpt-oss:20b, 13 runs): multi-step tool-continuation tasks ran up to 50% faster on 40% fewer
// input tokens, one-shot tasks were wall-neutral, and no deliverable regressed. Backends that do
// not render the field ignore it, so the cost of being on against the wrong model is bytes on
// the wire. MAGI_RESEND_REASONING=0 turns it off.
func resendReasoning() bool { return envflag.Enabled("MAGI_RESEND_REASONING", true) }

// toolResultContent renders a tool result as plain text for the wire. The result
// is stored as a JSON-encoded string, so unwrap it (json.RawMessage `"text"` →
// text) rather than sending the doubly-quoted literal to the model.
func toolResultContent(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return string(raw)
}
