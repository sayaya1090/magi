package openai

import (
	"encoding/json"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// ---- request wire format (OpenAI Chat Completions) ----

type chatRequest struct {
	Model         string         `json:"model"`
	Stream        bool           `json:"stream"`
	Messages      []wireMessage  `json:"messages"`
	Tools         []wireTool     `json:"tools,omitempty"`
	StreamOptions *streamOptions `json:"stream_options,omitempty"`
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

type wireUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// buildRequest converts a port.ChatRequest into the wire request body. When
// cache is set, it attaches cache_control breakpoints to the (large, stable)
// system prompt and tool list so an Anthropic model behind LiteLLM caches that
// prefix instead of re-billing it every turn.
func buildRequest(r port.ChatRequest, stream, cache bool) chatRequest {
	out := chatRequest{Model: r.Model, Stream: stream}
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
	out.Messages = append(out.Messages, convertMessages(r.Messages)...)

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

// convertMessages maps domain messages (with parts) to OpenAI wire messages.
func convertMessages(msgs []session.Message) []wireMessage {
	msgs = repairToolOrdering(msgs)
	var out []wireMessage
	for _, m := range msgs {
		switch m.Role {
		case session.RoleUser, session.RoleSystem:
			out = append(out, wireMessage{Role: string(m.Role), Content: joinText(m.Parts)})
		case session.RoleAssistant:
			wm := wireMessage{Role: "assistant", Content: joinText(m.Parts)}
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
