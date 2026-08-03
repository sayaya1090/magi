package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// ProbeContextWindow asks the backend for `model`'s real context length, trying the
// conventions different OpenAI-compatible servers expose (best-effort, all on the caller's
// ctx — give it a short timeout). Returns (0, false) when nothing usable is found, so the
// caller falls back to the model registry / default. Covers:
//   - vLLM:    GET /v1/models  → data[].max_model_len (also context_length/context_window)
//   - LiteLLM: GET /model/info → data[].model_info.max_input_tokens (or max_tokens)
//   - Ollama:  GET  /api/ps    → models[].context_length  (what this server WILL serve)
//   - Ollama:  POST /api/show  → model_info["<arch>.context_length"]  (what the model was trained for)
//
// The two Ollama sources answer different questions and the runtime one has to win. /api/show
// reads the GGUF metadata — the architecture's trained length — while the server actually serves
// whatever num_ctx it was started with (OLLAMA_CONTEXT_LENGTH, a Modelfile PARAMETER). Ask a
// server capped at 96K and /api/show still says 262144, so a caller sizing a compaction trigger
// against it aims past a limit the backend will never reach and never compacts.
//
// /api/ps only lists models currently LOADED, so a probe before the first request falls through
// to /api/show and gets the trained length. That is the backend's constraint, not a choice: the
// runtime value does not exist anywhere until an instance is running.
//
// Plain OpenAI does not expose context length anywhere, so it correctly returns false.
func (c *Client) ProbeContextWindow(ctx context.Context, model string) (int, bool) {
	if w, ok := c.probeModelsEndpoint(ctx, model); ok {
		return w, true
	}
	if w, ok := c.probeLiteLLMInfo(ctx, model); ok {
		return w, true
	}
	if w, ok := c.probeOllamaPS(ctx, model); ok {
		return w, true
	}
	if w, ok := c.probeOllamaShow(ctx, model); ok {
		return w, true
	}
	return 0, false
}

// probeOllamaPS reads the running instance's own context length from GET /api/ps. Empty
// (nothing loaded, or this model is not among them) yields false so the caller falls through.
func (c *Client) probeOllamaPS(ctx context.Context, model string) (int, bool) {
	host := strings.TrimSuffix(c.base(), "/v1")
	out, ok := c.getJSON(ctx, http.MethodGet, host+"/api/ps", nil)
	if !ok {
		return 0, false
	}
	list, _ := out["models"].([]any)
	for _, it := range list {
		m, _ := it.(map[string]any)
		if m == nil || !ollamaNameMatches(m, model) {
			continue
		}
		if w, ok := asInt(m["context_length"]); ok {
			return w, true
		}
	}
	return 0, false
}

// ollamaNameMatches reports whether a /api/ps entry is the model asked about. Ollama answers with
// the fully qualified name, so a request for "qwen3-coder-next" has to match "qwen3-coder-next:latest"
// — otherwise the untagged form every config file uses would never find its own running instance.
func ollamaNameMatches(entry map[string]any, model string) bool {
	for _, key := range []string{"model", "name"} {
		got, _ := entry[key].(string)
		if got == "" {
			continue
		}
		if got == model || strings.TrimSuffix(got, ":latest") == strings.TrimSuffix(model, ":latest") {
			return true
		}
	}
	return false
}

// getJSON performs an authenticated request and decodes a JSON object into a generic map.
func (c *Client) getJSON(ctx context.Context, method, url string, body []byte) (map[string]any, bool) {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, r)
	if err != nil {
		return nil, false
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	c.applyExtraHeaders(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	var out map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return nil, false
	}
	return out, true
}

// probeModelsEndpoint reads GET /v1/models and looks for a context-length field
// (vLLM's max_model_len, or context_length/context_window). It prefers the entry whose
// id matches model exactly; failing that — the common case where a single-model server
// (e.g. vLLM without --served-model-name) advertises the model under a full HF path that
// differs from the short alias the client was launched with — it falls back to the sole
// entry that carries a context field. The fallback stays strict when several models are
// served: with more than one candidate the id is genuinely ambiguous, so it reports none.
func (c *Client) probeModelsEndpoint(ctx context.Context, model string) (int, bool) {
	out, ok := c.getJSON(ctx, http.MethodGet, c.base()+"/models", nil)
	if !ok {
		return 0, false
	}
	data, _ := out["data"].([]any)
	var soleWindow, candidates int
	for _, e := range data {
		m, _ := e.(map[string]any)
		if m == nil {
			continue
		}
		w, hasWindow := modelEntryWindow(m)
		if hasWindow && asString(m["id"]) == model {
			return w, true // exact id match wins
		}
		if hasWindow {
			soleWindow = w
			candidates++
		}
	}
	if candidates == 1 {
		return soleWindow, true // single-model server whose id differs from the alias
	}
	return 0, false
}

// modelEntryWindow extracts a context-window value from one /v1/models entry, trying the
// field names different OpenAI-compatible servers use.
func modelEntryWindow(m map[string]any) (int, bool) {
	for _, k := range []string{"max_model_len", "context_length", "context_window", "max_context_length"} {
		if w, ok := asInt(m[k]); ok {
			return w, true
		}
	}
	return 0, false
}

// probeLiteLLMInfo reads LiteLLM's GET /model/info and reads model_info.max_input_tokens
// for the entry whose model_name matches model.
func (c *Client) probeLiteLLMInfo(ctx context.Context, model string) (int, bool) {
	// /model/info lives at the gateway root, not under /v1.
	base := strings.TrimSuffix(c.base(), "/v1")
	out, ok := c.getJSON(ctx, http.MethodGet, base+"/model/info", nil)
	if !ok {
		return 0, false
	}
	data, _ := out["data"].([]any)
	for _, e := range data {
		m, _ := e.(map[string]any)
		if m == nil || asString(m["model_name"]) != model {
			continue
		}
		info, _ := m["model_info"].(map[string]any)
		for _, k := range []string{"max_input_tokens", "max_tokens"} {
			if w, ok := asInt(info[k]); ok {
				return w, true
			}
		}
	}
	return 0, false
}

// probeOllamaShow reads Ollama's native POST /api/show and finds the "<arch>.context_length"
// entry in model_info.
func (c *Client) probeOllamaShow(ctx context.Context, model string) (int, bool) {
	host := strings.TrimSuffix(c.base(), "/v1")
	body, _ := json.Marshal(map[string]string{"model": model})
	out, ok := c.getJSON(ctx, http.MethodPost, host+"/api/show", body)
	if !ok {
		return 0, false
	}
	info, _ := out["model_info"].(map[string]any)
	for k, v := range info {
		if strings.HasSuffix(k, ".context_length") || k == "context_length" {
			if w, ok := asInt(v); ok {
				return w, true
			}
		}
	}
	return 0, false
}

func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		if n > 0 {
			return int(n), true
		}
	case json.Number:
		if i, err := n.Int64(); err == nil && i > 0 {
			return int(i), true
		}
	}
	return 0, false
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}
