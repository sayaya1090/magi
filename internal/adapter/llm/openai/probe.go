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
//   - Ollama:  POST /api/show  → model_info["<arch>.context_length"]
//
// Plain OpenAI does not expose context length anywhere, so it correctly returns false.
func (c *Client) ProbeContextWindow(ctx context.Context, model string) (int, bool) {
	if w, ok := c.probeModelsEndpoint(ctx, model); ok {
		return w, true
	}
	if w, ok := c.probeLiteLLMInfo(ctx, model); ok {
		return w, true
	}
	if w, ok := c.probeOllamaShow(ctx, model); ok {
		return w, true
	}
	return 0, false
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

// probeModelsEndpoint reads GET /v1/models and looks for a context-length field on the
// entry whose id matches model (vLLM's max_model_len, or context_length/context_window).
func (c *Client) probeModelsEndpoint(ctx context.Context, model string) (int, bool) {
	out, ok := c.getJSON(ctx, http.MethodGet, c.base()+"/models", nil)
	if !ok {
		return 0, false
	}
	data, _ := out["data"].([]any)
	for _, e := range data {
		m, _ := e.(map[string]any)
		if m == nil || asString(m["id"]) != model {
			continue
		}
		for _, k := range []string{"max_model_len", "context_length", "context_window", "max_context_length"} {
			if w, ok := asInt(m[k]); ok {
				return w, true
			}
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
