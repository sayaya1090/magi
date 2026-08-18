package app

import (
	"context"
	"encoding/json"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// What is in a companion's head right now, and what was folded away to make room.
//
// ContextView already answers this for the terminal, but it answers it as a paragraph of text: the
// numbers are formatted into a string by the same function that computes them, so a second surface
// can only re-derive or scrape. This is the same reading, structured — and it is the reading a
// SUPERVISOR needs rather than the operator's: not "how full is it" alone, but how many times this
// session has already had its history summarised away, and which topics went with it.
//
// # Why compaction is the thing to show
//
// A compaction is the one moment a companion silently stops knowing something. The summary keeps
// the gist and the shards keep the detail on disk, but between them sits a lossy step nobody is
// told about — and a companion that has compacted four times in one session is one whose earlier
// reasoning a person should not assume is still there. That is a supervision fact, and until now
// it existed only inside the log.
//
// # Derived, like everything else here
//
// From events already on disk: the compaction facts carry their own before/after sizes, and the
// last turn's usage carries the provider's real prompt count. No new event, so this answers for
// sessions that ran before it was written.
type ContextState struct {
	Model string `json:"model,omitempty"`
	// Window is the model's context window in tokens, 0 when this process does not know it. A
	// console loads no model config of its own, so 0 is common and means "no percentage can be
	// honestly shown" — not "unlimited".
	Window int `json:"window"`
	// Used is the size of what would be sent now: the provider's own prompt count from the last
	// finished turn when the log has one, else a ~4-chars-per-token estimate over the rebuilt
	// transcript. Estimated says which, because a person deciding whether to compact should know
	// whether they are looking at a measurement or arithmetic.
	Used      int  `json:"used"`
	Estimated bool `json:"estimated"`
	Messages  int  `json:"messages"`

	// Cached is how much of the last measured prompt the backend served from its own cache, and
	// CacheReported whether it said anything about a cache at all. The distinction is the whole
	// point: 0 means the cache missed, silence means this backend does not report it, and a screen
	// that drew both as 0% would report a working cache as broken. Measured on the default local
	// backend: it reports nothing, so this is usually silence.
	Cached        int  `json:"cached,omitempty"`
	CacheReported bool `json:"cacheReported,omitempty"`

	Compactions int       `json:"compactions"`
	Shed        int       `json:"shed"` // tokens the compactions freed, summed
	LastAt      time.Time `json:"lastAt,omitzero"`
	LastBefore  int       `json:"lastBefore,omitempty"`
	LastAfter   int       `json:"lastAfter,omitempty"`
	// Topics are the last compaction's shards: the file-shaped subjects whose full detail is off
	// in the log and can be pulled back with recall_context. They are what "the detail is not
	// lost" means concretely, and naming them is the difference between that claim and a promise.
	Topics []string `json:"topics,omitempty"`
}

// ContextStateOf reads one session's context situation.
func (a *App) ContextStateOf(ctx context.Context, sid session.SessionID) (ContextState, error) {
	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		return ContextState{}, err
	}
	msgs := reconstruct(evs)
	s := a.sessionInfo(ctx, sid)
	// From the events THIS call already read, not from the cached meta. A reader process caches a
	// session's meta on first sight and has nothing to invalidate it with — the daemon changes the
	// model, writes the fact, and the console kept answering from the snapshot it took before that.
	// The log is the source; this function is already holding it.
	model := s.Model.Model
	if m := modelFromEvents(evs); m != "" {
		model = m
	}
	out := ContextState{Model: model, Window: a.contextWindow(model), Messages: len(msgs)}

	for _, e := range evs {
		switch e.Type {
		case event.TypeCompaction:
			var d event.CompactionData
			if json.Unmarshal(e.Data, &d) != nil {
				continue
			}
			out.Compactions++
			if shed := d.TokensBefore - d.TokensAfter; shed > 0 {
				out.Shed += shed
			}
			out.LastAt, out.LastBefore, out.LastAfter = e.TS, d.TokensBefore, d.TokensAfter
			// A real prompt count measured before the fold describes a context that no longer
			// exists, and it is the LARGER number — carrying it across would report a companion
			// as nearly full at the exact moment it was emptied.
			out.Used, out.Cached, out.CacheReported = 0, 0, false
			out.Topics = out.Topics[:0]
			for _, sh := range d.Shards {
				out.Topics = append(out.Topics, sh.Topic)
			}
		case event.TypeTurnFinished:
			var d event.TurnFinishedData
			if json.Unmarshal(e.Data, &d) != nil {
				continue
			}
			// The provider's own count, from the most recent turn that reported one. A zero is
			// not a measurement — several backends omit usage — so it does not displace an
			// earlier real number or the estimate.
			if d.Usage.In > 0 {
				out.Used = d.Usage.In
				out.Cached, out.CacheReported = d.Usage.Cached, d.Usage.CacheReported
			}
		}
	}
	if out.Used == 0 {
		out.Used, out.Estimated = estimateTokens("", msgs), true
	}
	return out, nil
}
