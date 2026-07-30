package main

import "github.com/sayaya1090/magi/internal/config"

// resolveWindowOverride decides the context window and output budget to register for the running
// model, given whether magi already has metadata for it and what the operator put in [limits].
//
// probe is called ONLY when magi has no metadata for the model — asking a backend for a window it
// already knows is a request nobody needs, and the probe costs a round trip at startup.
//
// An explicit [limits] setting is the operator's answer and applies to EVERY model. It used to be
// read only inside the "magi has never heard of this model" branch, so `context_tokens` was
// silently ignored for all sixteen seeded ids — including magi's own default,
// `gpt-oss:120b-cloud`. Setting it changed nothing, said nothing, and left the compaction sizing
// on the seeded number. A setting the harness declines has to say so or be honoured; this
// honours it.
func resolveWindowOverride(seeded bool, probe func() (int, bool), lim config.LimitsConfig) (window, maxOut int, ok bool) {
	if !seeded {
		window, ok = probe()
	}
	if lim.ContextTokens > 0 {
		window, ok = lim.ContextTokens, true
	}
	if !ok {
		return 0, 0, false
	}
	// A quarter of the window is the fallback headroom; an explicit cap replaces it.
	maxOut = window / 4
	if lim.MaxOutputTokens > 0 {
		maxOut = lim.MaxOutputTokens
	}
	return window, maxOut, true
}
