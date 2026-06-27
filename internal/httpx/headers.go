// Package httpx holds small, dependency-free HTTP helpers shared by the
// adapters (the MCP transport and the OpenAI-compatible LLM client) so the
// static+dynamic custom-header logic lives in exactly one place.
package httpx

import (
	"net/http"
	"sync"
)

// Headers merges a static header set with any number of per-request providers
// onto an outgoing request. Providers are evaluated fresh on every Apply (so a
// rotating value such as an SSO token reflects request time) and overlay the
// static set; among providers, later ones win. Safe for concurrent Apply and
// AddProvider (a plugin may register a provider during a live session).
type Headers struct {
	mu        sync.Mutex
	static    map[string]string
	providers []func() map[string]string
}

// NewHeaders returns a header set seeded with static (may be nil). The map is
// copied so the caller can't mutate it afterwards.
func NewHeaders(static map[string]string) *Headers {
	h := &Headers{}
	if len(static) > 0 {
		h.static = make(map[string]string, len(static))
		for k, v := range static {
			h.static[k] = v
		}
	}
	return h
}

// AddStatic merges additional static headers (e.g. from a second config source).
func (h *Headers) AddStatic(m map[string]string) {
	if len(m) == 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.static == nil {
		h.static = map[string]string{}
	}
	for k, v := range m {
		h.static[k] = v
	}
}

// AddProvider registers a function re-evaluated on every Apply.
func (h *Headers) AddProvider(fn func() map[string]string) {
	if fn == nil {
		return
	}
	h.mu.Lock()
	h.providers = append(h.providers, fn)
	h.mu.Unlock()
}

// Empty reports whether there are no static headers and no providers.
func (h *Headers) Empty() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.static) == 0 && len(h.providers) == 0
}

// Apply sets static then provider headers on req. Call it after any protocol
// headers the caller wants to protect, or before them to let custom headers be
// overridden — the caller controls ordering by where it invokes Apply.
func (h *Headers) Apply(req *http.Request) {
	h.mu.Lock()
	static := h.static
	providers := append([]func() map[string]string(nil), h.providers...)
	h.mu.Unlock()
	for k, v := range static {
		req.Header.Set(k, v)
	}
	for _, fn := range providers {
		for k, v := range fn() {
			req.Header.Set(k, v)
		}
	}
}
