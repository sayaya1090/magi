// Package model is a registry of model metadata (context window, capabilities,
// pricing). It drives context-aware compaction, cost accounting, and multi-agent
// model routing (M6). Pricing is USD per 1M tokens; local models are free.
package model

import "sync"

// Info describes a model's capabilities and cost.
type Info struct {
	ID            string
	ContextWindow int     // max input tokens
	MaxOutput     int     // max output tokens
	Tools         bool    // supports tool/function calling
	Vision        bool    // supports image input
	InputCost     float64 // USD per 1M input tokens
	OutputCost    float64 // USD per 1M output tokens
}

// Cost returns the USD cost for the given token counts.
func (i Info) Cost(in, out int) float64 {
	return float64(in)/1e6*i.InputCost + float64(out)/1e6*i.OutputCost
}

// Registry maps model ids to metadata. Safe for concurrent use.
type Registry struct {
	mu     sync.RWMutex
	models map[string]Info
}

// NewRegistry returns a registry seeded with well-known models.
func NewRegistry() *Registry {
	r := &Registry{models: map[string]Info{}}
	for _, m := range defaults {
		r.models[m.ID] = m
	}
	return r
}

// Register adds or replaces a model's metadata (used by static plugin
// contributions, D10).
func (r *Registry) Register(m Info) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.models[m.ID] = m
}

// Has reports whether id is known.
func (r *Registry) Has(id string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.models[id]
	return ok
}

// Get returns metadata for id, falling back to a conservative default for
// unknown models so callers always get usable numbers.
func (r *Registry) Get(id string) Info {
	r.mu.RLock()
	m, ok := r.models[id]
	r.mu.RUnlock()
	if ok {
		return m
	}
	return Info{ID: id, ContextWindow: 8192, MaxOutput: 4096, Tools: true}
}

// defaults are the seeded models. Local (Ollama) models are free.
var defaults = []Info{
	// Local / open models.
	{ID: "qwen3-coder:30b", ContextWindow: 262144, MaxOutput: 65536, Tools: true},
	{ID: "qwen2.5-coder:32b", ContextWindow: 32768, MaxOutput: 8192, Tools: true},
	{ID: "gpt-oss:20b", ContextWindow: 131072, MaxOutput: 32768, Tools: true},
	{ID: "gpt-oss:120b", ContextWindow: 131072, MaxOutput: 32768, Tools: true},
	{ID: "gpt-oss:120b-cloud", ContextWindow: 131072, MaxOutput: 32768, Tools: true},
	{ID: "llama3.1:8b", ContextWindow: 131072, MaxOutput: 8192, Tools: true},
	{ID: "devstral:24b", ContextWindow: 131072, MaxOutput: 8192, Tools: true},
	{ID: "gemma3:27b", ContextWindow: 131072, MaxOutput: 8192, Tools: true},
	// Hosted (for when a remote OpenAI-compatible / native endpoint is used).
	{ID: "gpt-4o", ContextWindow: 128000, MaxOutput: 16384, Tools: true, Vision: true, InputCost: 2.5, OutputCost: 10},
	{ID: "claude-opus-4-8", ContextWindow: 200000, MaxOutput: 32000, Tools: true, Vision: true, InputCost: 5, OutputCost: 25},
	{ID: "claude-sonnet-4-6", ContextWindow: 200000, MaxOutput: 64000, Tools: true, Vision: true, InputCost: 3, OutputCost: 15},
	// Gemini (context length isn't in its OpenAI-compat /models, so it's seeded here).
	{ID: "gemini-2.5-pro", ContextWindow: 1048576, MaxOutput: 65536, Tools: true, Vision: true},
	{ID: "gemini-2.0-flash", ContextWindow: 1048576, MaxOutput: 8192, Tools: true, Vision: true},
	{ID: "gemini-1.5-pro", ContextWindow: 2097152, MaxOutput: 8192, Tools: true, Vision: true},
	// Grok (xAI); /v1/models doesn't expose context length.
	{ID: "grok-2", ContextWindow: 131072, MaxOutput: 8192, Tools: true},
	{ID: "grok-3", ContextWindow: 131072, MaxOutput: 8192, Tools: true},
	{ID: "grok-4", ContextWindow: 256000, MaxOutput: 16384, Tools: true, Vision: true},
}
