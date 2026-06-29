// Package openai implements port.LLMProvider against any OpenAI-compatible
// Chat Completions endpoint. Swapping the base URL covers Ollama (local, D3),
// vLLM, LiteLLM, OpenRouter, and OpenAI itself.
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/httpx"
	"github.com/sayaya1090/magi/internal/port"
)

// Client is an OpenAI-compatible LLM provider.
type Client struct {
	baseURL  string
	dynBase  atomic.Pointer[string] // runtime override (plugin magi.set_base_url); nil = use baseURL
	apiKey   string
	http     *http.Client
	cache    bool        // attach cache_control breakpoints (Anthropic via LiteLLM)
	cacheOff atomic.Bool // set after a backend rejects the cache shape (sticky fallback)

	headers *httpx.Headers // static (config) + dynamic (plugin) custom headers
}

// base returns the effective base URL: a runtime override (set by a plugin via
// magi.set_base_url) if present, else the configured one. Read on every request so a
// plugin can redirect the LLM backend mid-session.
func (c *Client) base() string {
	if p := c.dynBase.Load(); p != nil && *p != "" {
		return *p
	}
	return c.baseURL
}

// SetBaseURL overrides the LLM backend base URL at runtime (e.g. a plugin pointing the
// agent at a local proxy/mock). An empty string clears the override. Safe to call while
// the client is in use.
func (c *Client) SetBaseURL(url string) {
	u := strings.TrimRight(url, "/")
	c.dynBase.Store(&u)
}

// describeStatus maps an HTTP status to a short, actionable cause for users
// behind a gateway like LiteLLM (is it auth, a missing model, or a bad request?).
func describeStatus(status int) string {
	switch status {
	case 400, 422:
		return "bad request — check the model name, parameters, or message format"
	case 401:
		return "unauthorized — check the API key (MAGI_API_KEY)"
	case 403:
		return "forbidden — the gateway denied this key/model (permissions)"
	case 404:
		return "not found — check -model and -base-url (model or endpoint missing)"
	case 408, 504:
		return "upstream timeout — the gateway/model took too long"
	case 429:
		return "rate limited"
	case 502, 503:
		return "gateway unavailable — the backend/model is down or overloaded"
	}
	if status >= 500 {
		return "server error"
	}
	return "request rejected"
}

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient overrides the default HTTP client.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// WithHeaders adds static custom headers sent on every request — e.g. an in-house
// gateway's X-CLIENT-API-KEY. Set from config ([llm].headers); values should be
// resolved (env-expanded) by the caller.
func WithHeaders(h map[string]string) Option {
	return func(c *Client) { c.headers.AddStatic(h) }
}

// AddLLMHeaders registers a header provider re-evaluated on EVERY request, so a
// plugin can supply values that change between requests (rotating SSO tokens,
// timestamps). Safe to call while the client is in use (e.g. plugin hot-reload).
func (c *Client) AddLLMHeaders(fn func() map[string]string) { c.headers.AddProvider(fn) }

// applyExtraHeaders overlays static (config) then dynamic (plugin) headers onto a
// request. Called after the protocol/auth headers so a caller can intentionally
// override Authorization (e.g. swap in an SSO token).
func (c *Client) applyExtraHeaders(req *http.Request) { c.headers.Apply(req) }

// WithPromptCache enables cache_control breakpoints on the system prompt and
// tools. Use only when the backend (e.g. an Anthropic model behind LiteLLM)
// honors them; harmless caches are ignored by providers that don't.
func WithPromptCache() Option { return func(c *Client) { c.cache = true } }

// WithResponseHeaderTimeout bounds how long to wait for the response headers
// after sending a request. Headers arrive before any token, so this catches a
// gateway/model that never starts responding WITHOUT cutting a slow token stream
// (use behind a gateway that may hang). d<=0 leaves it unbounded.
func WithResponseHeaderTimeout(d time.Duration) Option {
	return func(c *Client) {
		if d <= 0 {
			return
		}
		tr := http.DefaultTransport.(*http.Transport).Clone()
		tr.ResponseHeaderTimeout = d
		c.http = &http.Client{Timeout: 0, Transport: tr} // 0: don't cap the stream body
	}
}

// New returns a Client for the given base URL (e.g. "http://localhost:11434/v1").
// apiKey may be empty for local backends like Ollama.
func New(baseURL, apiKey string, opts ...Option) *Client {
	c := &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 0}, // streaming: no overall timeout
		headers: httpx.NewHeaders(nil),
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// StreamChat sends a streaming chat completion request and returns a channel of
// normalized provider events. A connection-level failure (e.g. bad base URL) is
// returned immediately (F-LLM-ERROR llm-err-3); server/stream errors surface as
// ProviderError events on the channel (llm-err-1, llm-err-2).
func (c *Client) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	useCache := c.cache && !c.cacheOff.Load()
	var resp *http.Response
	var lastStatus int
	var lastBody string
	var lastErr error
	for {
		body, merr := json.Marshal(buildRequest(r, true, useCache))
		if merr != nil {
			return nil, merr
		}
		resp, lastStatus, lastBody, lastErr = c.send(ctx, body)
		// Auto-fallback: a cached request rejected as a bad request (400/422) likely
		// means the backend doesn't accept the cache_control content shape. Retry
		// once without caching and remember it for the rest of the session, so
		// caching can stay default-on without breaking non-supporting backends.
		if resp != nil && useCache && (resp.StatusCode == 400 || resp.StatusCode == 422) {
			resp.Body.Close()
			c.cacheOff.Store(true)
			useCache = false
			continue
		}
		break
	}
	if resp == nil {
		if lastErr != nil {
			return nil, lastErr // connection-level / context failure
		}
		if lastStatus != 0 {
			return nil, fmt.Errorf("llm: %s (status %d): %s", describeStatus(lastStatus), lastStatus, lastBody)
		}
		return nil, fmt.Errorf("llm: request failed")
	}

	known := make(map[string]bool, len(r.Tools))
	for _, t := range r.Tools {
		known[t.Name] = true
	}

	ch := make(chan port.ProviderEvent)
	go func() {
		defer close(ch)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			msg, _ := io.ReadAll(io.LimitReader(resp.Body, 8*1024))
			emit(ctx, ch, port.ProviderEvent{
				Type: port.ProviderError,
				Err:  fmt.Errorf("llm: %s (status %d): %s", describeStatus(resp.StatusCode), resp.StatusCode, strings.TrimSpace(string(msg))),
			})
			return
		}
		c.consume(ctx, resp.Body, ch, known)
	}()
	return ch, nil
}

// ListModels fetches the backend's model catalog (GET /models). Useful behind a
// gateway like LiteLLM where the available models change and users shouldn't have
// to memorize IDs.
func (c *Client) ListModels(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base()+"/models", nil)
	if err != nil {
		return nil, err
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	c.applyExtraHeaders(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2*1024))
		return nil, fmt.Errorf("llm: %s (status %d): %s", describeStatus(resp.StatusCode), resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(out.Data))
	for _, m := range out.Data {
		if m.ID != "" {
			ids = append(ids, m.ID)
		}
	}
	return ids, nil
}

// send issues the request with bounded retries on transient failures (connection
// errors, 429, 5xx) and honored Retry-After. It returns the live response (when
// the final status is non-retryable), or the last status/body/error for the
// caller to render.
func (c *Client) send(ctx context.Context, body []byte) (resp *http.Response, lastStatus int, lastBody string, err error) {
	const maxAttempts = 3
	var retryAfter time.Duration
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			d := time.Duration(300*(1<<(attempt-1))) * time.Millisecond
			if retryAfter > 0 {
				d = retryAfter
			}
			select {
			case <-time.After(d):
			case <-ctx.Done():
				return nil, lastStatus, lastBody, ctx.Err()
			}
		}
		req, rerr := http.NewRequestWithContext(ctx, http.MethodPost, c.base()+"/chat/completions", bytes.NewReader(body))
		if rerr != nil {
			return nil, 0, "", rerr
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")
		if c.apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+c.apiKey)
		}
		c.applyExtraHeaders(req)
		var doErr error
		resp, doErr = c.http.Do(req)
		if doErr == nil && !retryableStatus(resp.StatusCode) {
			return resp, resp.StatusCode, "", nil // success or non-retryable status
		}
		err = doErr
		if resp != nil {
			lastStatus = resp.StatusCode
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 2*1024))
			lastBody = strings.TrimSpace(string(b))
			retryAfter = parseRetryAfter(resp.Header.Get("Retry-After"))
			resp.Body.Close()
			resp = nil
		}
	}
	return nil, lastStatus, lastBody, err
}

// consume parses the SSE stream and emits normalized events. known is the set of
// offered tool names, used by the text fallback to recognize tool calls emitted
// as plain content (F-LLM-TOOLS-FALLBACK).
func (c *Client) consume(ctx context.Context, body io.Reader, ch chan<- port.ProviderEvent, known map[string]bool) {
	acc := newToolAccumulator()
	var fullText strings.Builder
	nativeCalls := false

	err := sseEvents(body, func(data []byte) error {
		var chunk streamChunk
		if err := json.Unmarshal(data, &chunk); err != nil {
			// Skip malformed lines; stream continues (F-LLM-SSE sse-4).
			return nil
		}
		if chunk.Usage != nil {
			emit(ctx, ch, port.ProviderEvent{
				Type:  port.ProviderUsage,
				Usage: &event.Usage{In: chunk.Usage.PromptTokens, Out: chunk.Usage.CompletionTokens},
			})
		}
		for _, choice := range chunk.Choices {
			if rt := choice.Delta.reasoningText(); rt != "" {
				emit(ctx, ch, port.ProviderEvent{Type: port.ProviderReasoning, Text: rt})
			}
			if choice.Delta.Content != "" {
				fullText.WriteString(choice.Delta.Content)
				emit(ctx, ch, port.ProviderEvent{Type: port.ProviderText, Text: choice.Delta.Content})
			}
			acc.add(choice.Delta.ToolCalls)
			if choice.FinishReason != nil {
				calls := acc.finish()
				if len(calls) > 0 {
					nativeCalls = true
				}
				for _, tc := range calls {
					emit(ctx, ch, port.ProviderEvent{Type: port.ProviderToolCall, ToolCall: tc})
				}
				// Fallback: no native tool_calls, but the text may itself be a
				// tool call (e.g. qwen2.5-coder via Ollama).
				if !nativeCalls && len(known) > 0 {
					tc, ok := parseFallbackToolCall(fullText.String(), known)
					if !ok {
						tc, ok = parseXMLToolCall(fullText.String(), known)
					}
					if ok {
						tc.CallID = fmt.Sprintf("call_fb_%d", time.Now().UnixNano())
						emit(ctx, ch, port.ProviderEvent{Type: port.ProviderToolCall, ToolCall: tc, FromText: true})
					}
				}
				emit(ctx, ch, port.ProviderEvent{Type: port.ProviderFinish})
			}
		}
		return nil
	})
	if err != nil {
		// Mid-stream read error: emit error; partial parts already emitted
		// upstream are preserved (F-LLM-ERROR llm-err-2).
		emit(ctx, ch, port.ProviderEvent{Type: port.ProviderError, Err: err})
	}
}

// retryableStatus reports whether an HTTP status warrants a retry.
func retryableStatus(code int) bool {
	switch code {
	case http.StatusTooManyRequests, // 429
		http.StatusInternalServerError, // 500
		http.StatusBadGateway,          // 502
		http.StatusServiceUnavailable,  // 503
		http.StatusGatewayTimeout:      // 504
		return true
	}
	return false
}

// parseRetryAfter reads a Retry-After header (delta-seconds form) into a duration,
// capped so a server can't park the UI for too long. Returns 0 if absent/invalid.
func parseRetryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	secs, err := strconv.Atoi(v)
	if err != nil || secs < 0 {
		return 0
	}
	d := time.Duration(secs) * time.Second
	if d > 15*time.Second {
		d = 15 * time.Second
	}
	return d
}

// emit sends ev unless ctx is cancelled (avoids blocking on a dropped consumer).
func emit(ctx context.Context, ch chan<- port.ProviderEvent, ev port.ProviderEvent) {
	select {
	case ch <- ev:
	case <-ctx.Done():
	}
}

// firstJSONValue returns the first complete JSON value in b, dropping any trailing
// data. Some OpenAI-compatible backends (e.g. minimax via LiteLLM) repeat the full
// arguments across deltas, yielding "{…}{…}" once concatenated — taking the first
// value recovers clean arguments instead of an "after top-level value" parse error.
func firstJSONValue(b []byte) json.RawMessage {
	dec := json.NewDecoder(bytes.NewReader(b))
	var raw json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return b // not decodable as a single value; leave as-is
	}
	return raw
}

// toolAccumulator assembles streamed tool_calls (whose arguments may arrive in
// fragments across chunks) into complete ToolCalls, keyed by index.
type toolAccumulator struct {
	order []int
	calls map[int]*session.ToolCall
}

func newToolAccumulator() *toolAccumulator {
	return &toolAccumulator{calls: make(map[int]*session.ToolCall)}
}

func (a *toolAccumulator) add(tcs []wireToolCall) {
	for _, tc := range tcs {
		cur, ok := a.calls[tc.Index]
		if !ok {
			cur = &session.ToolCall{}
			a.calls[tc.Index] = cur
			a.order = append(a.order, tc.Index)
		}
		if tc.ID != "" {
			cur.CallID = tc.ID
		}
		if tc.Function.Name != "" {
			cur.Name = tc.Function.Name
		}
		if tc.Function.Arguments != "" {
			cur.Args = append(cur.Args, tc.Function.Arguments...)
		}
	}
}

// finish returns the accumulated tool calls in arrival order, normalizing empty
// argument payloads to "{}".
func (a *toolAccumulator) finish() []*session.ToolCall {
	var out []*session.ToolCall
	for _, idx := range a.order {
		tc := a.calls[idx]
		if len(tc.Args) == 0 {
			tc.Args = json.RawMessage("{}")
		} else {
			tc.Args = firstJSONValue(tc.Args)
		}
		if tc.CallID == "" {
			tc.CallID = fmt.Sprintf("call_%d_%d", idx, time.Now().UnixNano())
		}
		out = append(out, tc)
	}
	// Prevent double emission if finish_reason appears more than once.
	a.order = nil
	a.calls = make(map[int]*session.ToolCall)
	return out
}
