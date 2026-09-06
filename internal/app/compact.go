package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// keepRecentEvents is the number of trailing fact events preserved verbatim when
// auto-compacting; older context is summarized.
const keepRecentEvents = 6

// maybeCompact summarizes older context when the estimated token count exceeds
// the model's window budget, returning true if a compaction event was appended.
// (M6 context-aware compaction, building on F-COMPACT.)
// msgs is reconstruct(evs), built once per step by the caller and shared (reconstruct is
// O(events); this sizing check runs every step).
func (a *App) maybeCompact(ctx context.Context, s session.Session, agent AgentSpec, actor event.Actor, evs []event.Event, msgs []session.Message, sys string) bool {
	window := a.contextWindow(s.Model.Model)
	if window <= 0 {
		return false // unknown/unlimited window → no ratio-based auto-compaction
	}
	budget := int(float64(window) * a.cfg.CompactRatio)
	// The provider's real prompt_tokens lags within a turn (it reflects the last completed
	// call), so as a turn accumulates large tool results the trigger would under-count and
	// miss the growth. Use whichever is larger — the real count or the current estimate.
	tokens := a.contextTokens(s.ID, sys, msgs)
	// The tool block rides on every request — names, descriptions and schemas, ~6-7k tokens on the
	// default roster — and the estimate never counted it, so on a backend that reports no real
	// prompt_tokens (many local ones) the meter and this trigger ran that much light and compaction
	// fired late. Cheap to add: summing lengths, no marshalling.
	if est := estimateTokens(sys, msgs) + toolSpecTokens(a.sessionToolSpecs(s.ID, agent)); est > tokens {
		tokens = est
	}
	if tokens <= budget {
		return false
	}
	// Folding can only shrink the messages, never the system prompt. When the messages ALONE
	// already fit the budget, the overage is the system prompt (or the real-count lag) and no fold
	// gets under it — without this guard the trigger fired every step, each paying a summarizer
	// call that reclaimed nothing: 18 folds for 25 tool calls was measured, messages sitting at
	// ~800 tokens against a 6000 budget while the prompt alone was ~6000. The backend's own limit
	// stays the authority there (the reactive fold on a provider rejection still runs); this stops
	// the estimate from spinning. Messages that are themselves over budget still fold.
	if estimateTokens("", msgs) <= budget {
		return false
	}
	// Before the fold, the cheaper cut: give up recent bulky tool results instead of the oldest
	// turns. A fold rewrites the head, so the cache re-bills everything behind it and a summariser
	// call is paid to preserve what cannot be re-derived; an elided result costs neither — the
	// replacement lands near the TAIL, and the bytes are re-derivable (the file is still on disk,
	// the command still runs). Only DIGESTED results qualify: if the assistant never narrated what
	// a result meant, eliding it deletes knowledge that exists nowhere else, and that is exactly
	// what the fold's summary is for. When eliding reclaims enough, the fold does not run at all;
	// when it cannot, the fold runs over what remains.
	if n, covered := a.elideRecentResults(ctx, s, actor, evs, estimateTokens("", msgs)-budget); n > 0 {
		if covered {
			return true
		}
		evs, _ = a.store.Read(ctx, s.ID, 0)
	}
	return a.compactNow(ctx, s, agent, actor, evs)
}

// elideMinBytes: below this a result is not worth a stub — the stub itself is ~150 bytes, and a
// small result may be load-bearing detail. Well above stub size, well below the bulky reads and
// build logs that actually fill windows.
const elideMinBytes = 2048

// The second tier of the cheap cut: results of READ-ONLY tools. The first tier takes only what the
// assistant narrated; a look that was never narrated is still re-derivable — the file is on disk,
// the sheet is in the workbook — so when narrated results do not cover the overage, old looks go
// next, OLDEST first (the model is working from the recent ones), keeping the newest
// keepFreshLooks untouched. The bar is lower than elideMinBytes because many small looks add up
// to the window that closed, and each stub still says how to have the bytes again.
const (
	elideLookMinBytes = 1024
	keepFreshLooks    = 3
)

// toolReadOnly is whether a tool's result can be had again by calling it again: the builtin looks,
// and any tool that declares it (an MCP server's `annotations.readOnlyHint`, port.ReadOnlyTool).
func (a *App) toolReadOnly(name string) bool {
	if readOnlyTools[name] {
		return true
	}
	if a.tools == nil {
		return false
	}
	t, ok := a.tools.Get(name)
	if !ok {
		return false
	}
	ro, can := t.(port.ReadOnlyTool)
	return can && ro.ReadOnly()
}

// elideRecentResults marks bulky, digested tool results as elided, newest first, until the
// overage is covered or candidates run out. Returns how many were marked and whether the
// estimated savings covered the overage.
//
// Newest-first is the cache arithmetic: replacing a result re-bills only what follows it, so the
// later the cut, the cheaper. The single newest result is exempt — it is what the model is about
// to act on. "Digested" means an assistant TEXT part follows the result somewhere before the next
// tool result: the model wrote down what it learned, so the raw bytes are redundant as well as
// re-derivable. An undigested result is left for the fold, whose summary supplies the digestion.
func (a *App) elideRecentResults(ctx context.Context, s session.Session, actor event.Actor, evs []event.Event, overTokens int) (int, bool) {
	if overTokens <= 0 {
		return 0, true
	}
	already := map[string]bool{}
	type cand struct {
		callID string
		bytes  int
	}
	var results []cand // every tool result, in order
	digested := map[string]bool{}
	toolOf := map[string]string{} // callID → tool name, for the read-only tier
	lastResult := ""              // callID of the newest result — exempt
	for _, ev := range evs {
		switch ev.Type {
		case event.TypeResultElided:
			var d event.ResultElidedData
			if json.Unmarshal(ev.Data, &d) == nil {
				already[d.CallID] = true
			}
		case event.TypePartAppended:
			var d event.PartAppendedData
			if json.Unmarshal(ev.Data, &d) != nil {
				continue
			}
			for _, p := range []session.Part{d.Part} {
				if p.ToolCall != nil {
					toolOf[p.ToolCall.CallID] = p.ToolCall.Name
				}
				if p.ToolResult != nil {
					results = append(results, cand{p.ToolResult.CallID, len(p.ToolResult.Content)})
					lastResult = p.ToolResult.CallID
				}
				if p.Kind == session.PartText && d.Role == session.RoleAssistant && len(results) > 0 {
					digested[results[len(results)-1].callID] = true
				}
			}
		}
	}
	saved, n := 0, 0
	for i := len(results) - 1; i >= 0; i-- {
		c := results[i]
		if c.callID == lastResult || c.callID == "" || already[c.callID] ||
			!digested[c.callID] || c.bytes < elideMinBytes {
			continue
		}
		d, _ := json.Marshal(event.ResultElidedData{CallID: c.callID, Bytes: c.bytes})
		if a.appendFact(ctx, s.ID, event.TypeResultElided, actor, d) != nil {
			break
		}
		already[c.callID] = true
		saved += c.bytes / 4
		n++
		if saved >= overTokens {
			break
		}
	}
	// Second tier: old read-only looks, oldest first, the newest keepFreshLooks spared.
	looks := 0
	if saved < overTokens {
		var reads []cand
		for _, c := range results {
			if c.callID == lastResult || c.callID == "" || already[c.callID] ||
				c.bytes < elideLookMinBytes || !a.toolReadOnly(toolOf[c.callID]) {
				continue
			}
			reads = append(reads, c)
		}
		for i := 0; i < len(reads)-keepFreshLooks && saved < overTokens; i++ {
			c := reads[i]
			if already[c.callID] {
				continue // taken by the first tier in this same pass
			}
			d, _ := json.Marshal(event.ResultElidedData{CallID: c.callID, Bytes: c.bytes})
			if a.appendFact(ctx, s.ID, event.TypeResultElided, actor, d) != nil {
				break
			}
			already[c.callID] = true
			saved += c.bytes / 4
			n++
			looks++
		}
	}
	if n > 0 {
		what := "re-derivable, and already narrated"
		if looks > 0 {
			what = fmt.Sprintf("%d narrated, %d old read-only looks that can be taken again", n-looks, looks)
		}
		a.emitToolProgress(s.ID, actor, "", "compact",
			fmt.Sprintf("freed the window by eliding %d bulky tool result(s) — %s", n, what))
	}
	return n, saved >= overTokens
}

// compactNow folds older context into a summary down to the last keepRecentEvents facts,
// regardless of any window/budget estimate. maybeCompact calls it once its ratio gate trips;
// the stream loop calls it directly when the provider ITSELF rejects a request as too long —
// the backend's own limit is ground truth that overrides our token estimate (which can under-
// count, or be uncalibrated when the model's real window differs from a hand-set constant).
// Returns false when there is nothing left to fold (already at the minimal kept tail).
func (a *App) compactNow(ctx context.Context, s session.Session, agent AgentSpec, actor event.Actor, evs []event.Event) bool {
	// Find the boundary: keep the last keepRecentEvents fact events verbatim.
	factSeqs := make([]int64, 0, len(evs))
	for _, e := range evs {
		if e.Type.IsFact() {
			factSeqs = append(factSeqs, e.Seq)
		}
	}
	if len(factSeqs) <= keepRecentEvents+1 {
		return false // not enough to compact
	}
	boundary := factSeqs[len(factSeqs)-keepRecentEvents-1]
	// The tail is measured in TOKENS, not events, when the window is known: keep the most recent
	// turns that fit CompactKeep of the budget, and fold everything before them. Six events was
	// half the window after a run of bulky results and next to nothing after a run of one-line
	// turns; either way the person saw a fold that kept the wrong amount. The six-event floor
	// stays as the least that is ever kept.
	if keep := a.keepTailTokens(s); keep > 0 {
		if b := tailBoundary(evs, keep); b > 0 && b < boundary {
			boundary = b
		}
	}
	// Never fold BELOW the previous fold's own event. With few turns since it, the event floor
	// landed inside that fold's kept tail: the region folded was that tail alone, the previous
	// brief (a later seq) survived the new snapshot as a second brief UNDER the new one, and the
	// summariser never saw it — measured 2026-09-07 (Excel, two forced folds eight events apart:
	// the view read brief-2, brief-1, tail, newest history first). From the previous fold's seq
	// upward, the region is that brief plus everything since, which is what accumulates.
	if c := lastCompactionSeq(evs); c > boundary {
		boundary = c
	}
	// The boundary must not fall INSIDE a message. One step writes reasoning, text and tool
	// calls as separate PartAppended events under one message id, and a boundary chosen by
	// counting fact events lands between them routinely — the fold then dropped the whole
	// entry (its OPENING seq is under the boundary) with the post-boundary parts merged in,
	// while the summarizer's input was cut at the boundary and never saw them either: an
	// assistant's tool call vanished from both sides, its result surviving as an orphan.
	// Snapped DOWN to the nearest seam — keeping more, never losing — and a snap that
	// reaches zero means the log is one giant message: nothing safe to fold.
	snapped := snapToMessageSeam(evs, boundary)
	older := reconstruct(truncateAt(evs, snapped))
	if len(older) == 0 {
		// Lowering left nothing to fold — the straddling message reaches (nearly) to the head
		// of the log, so everything foldable is inside it. Folding a message WHOLE is also not
		// splitting it: raise to its far side instead, trading some of the kept tail for a
		// fold that still happens.
		snapped = raiseToMessageSeam(evs, boundary)
		older = reconstruct(truncateAt(evs, snapped))
		if len(older) == 0 {
			return false
		}
	}
	// A fold must actually fold. The snap can land on (or below) the previous compaction's own
	// boundary, and then truncateAt drops that compaction event too: the summariser is handed the
	// same raw pre-fold history again, the new snapshot keeps exactly what the old one kept, and
	// the context comes back LARGER by one summary. compactNow returning true then reads as a
	// successful compaction, so the overflow retry does it again — three rounds, three summariser
	// calls over the whole history, nothing reclaimed.
	if snapped <= lastCompactionBoundary(evs) {
		return false
	}
	boundary = snapped
	// Index the compacted region into recallable topics (deterministic — by file path,
	// each carrying its tool-action trail as a brief), then write the overall summary.
	shards := shardBy(older, s.Workdir, topicKeysOf(a.toolSpecs(s.ID, agent)))
	// Say it is happening. Compaction is a model call on a large prompt — measured in tens of
	// seconds on a local backend — and it runs BETWEEN steps, so nothing else is being drawn: the
	// transcript simply stops. From the outside that is indistinguishable from a wedged turn, and
	// it is the one long pause on this page that had no line to explain it. Tool calls have said
	// what they are doing all along; this rides the same channel, which is also the only one a
	// browser in another process can read (noteDoing).
	a.emitToolProgress(s.ID, actor, "", "compact",
		fmt.Sprintf("compacting the conversation — summarising %d earlier messages", len(older)))
	// The summary ACCUMULATES. The folded region holds the previous fold's brief as its first
	// message, and summarising it again with the new turns made every fold a summary of a summary:
	// four folds in, the opening decisions were a paraphrase of a paraphrase. The previous brief is
	// kept verbatim and the new turns are folded after it; only when the running brief outgrows
	// its share of the window is it condensed, once, as a whole.
	prior := priorSummary(evs, boundary)
	summary := a.summarizeViaLLM(ctx, agent, s, withoutBriefs(older))
	if summary == "" {
		a.emitToolProgress(s.ID, actor, "", "compact", "compaction did not produce a summary — keeping the conversation as it is")
		return false
	}
	if prior != "" {
		summary = prior + "\n\n" + summary
	}
	if cap := a.briefCapTokens(s); cap > 0 && len(summary)/4 > cap {
		a.emitToolProgress(s.ID, actor, "", "compact",
			fmt.Sprintf("the running brief outgrew its share (%d tokens over %d) — condensing it", len(summary)/4, cap))
		if condensed := a.condenseBrief(ctx, agent, s, summary); condensed != "" {
			summary = condensed
		}
	}

	// Post-compaction context = the summary + the kept recent events.
	var keptEvs []event.Event
	for _, e := range evs {
		if e.Seq > boundary {
			keptEvs = append(keptEvs, e)
		}
	}
	tokensBefore, tokensAfter := estimateTokens("", reconstruct(evs)), estimateTokens(summary, reconstruct(keptEvs))
	d, _ := json.Marshal(event.CompactionData{
		Summary:         summary,
		ReplacesUpToSeq: boundary,
		TokensBefore:    tokensBefore,
		TokensAfter:     tokensAfter,
		Shards:          shards,
	})
	a.appendFact(ctx, s.ID, event.TypeCompaction, actor, d)
	// And say what it cost. The numbers are the ones the fact carries, so the line and the record
	// cannot disagree — and "what did I just lose" is the question somebody has when they see the
	// transcript fold underneath them.
	a.emitToolProgress(s.ID, actor, "", "compact",
		fmt.Sprintf("compacted — %d → %d tokens, %d messages summarised, %d topics recallable",
			tokensBefore, tokensAfter, len(older), len(shards)))
	// The real prompt count is now a measurement of a context that no longer exists — and it is
	// the LARGER number, so the trigger (which takes max of the real count and the estimate) would
	// keep firing on the emptied window. The log-derived reader already zeroes it after a fold for
	// exactly this reason; the in-memory trigger was left reading the dead value and re-folding the
	// tail of what little remained, once per turn that ended before a fresh request could refresh
	// it. Cleared here so the next step measures the folded context.
	a.setPromptTokens(s.ID, 0)
	return true
}

// shardByPath groups the compacted messages into recallable topics by the file each
// one touched (read/edited/grepped/etc.), plus a single "discussion" shard for messages
// that reference no file. It is deterministic — no model call and no parse-failure path —
// so the index is always faithful and complete: every recoverable message lands in at
// least one shard, and a topic is a path the agent naturally recalls. Prior summaries
// (id "compaction-*") are skipped — they are not original detail to recover.
func shardByPath(older []session.Message, workdir string) []event.ContextShard {
	return shardBy(older, workdir, nil)
}

// topicKeysOf reads, per tool, which arguments the tool DECLARED as its topic — a schema property
// carrying `"x-magi-topic": true`. A file tool's topic was always its path; a sheet, a slide or a
// paragraph is the same kind of handle for a tool that works on a document, and the core cannot
// know which argument that is unless the tool says. The Office helper declares them.
func topicKeysOf(specs []port.ToolSpec) map[string][]string {
	out := map[string][]string{}
	for _, sp := range specs {
		if keys := topicArgsOf(sp.Schema); len(keys) > 0 {
			out[sp.Name] = keys
		}
	}
	return out
}

// topicArgsOf is the property names of a schema marked "x-magi-topic": true, sorted.
func topicArgsOf(schema json.RawMessage) []string {
	var sc struct {
		Properties map[string]map[string]any `json:"properties"`
	}
	if json.Unmarshal(schema, &sc) != nil {
		return nil
	}
	var out []string
	for name, prop := range sc.Properties {
		if v, ok := prop["x-magi-topic"].(bool); ok && v {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// callTopics is every topic one call names: the file path when it has one (any tool), and each
// declared topic argument as "<argument> <value>" — a string or number as it is, an array as one
// topic per element. Empty when the call names nothing.
func callTopics(workdir, name string, args json.RawMessage, topics map[string][]string) []string {
	var out []string
	if rel := shardPath(workdir, args); rel != "" {
		out = append(out, rel)
	}
	keys := topics[name]
	if len(keys) == 0 {
		return out
	}
	var raw map[string]any
	if json.Unmarshal(args, &raw) != nil {
		return out
	}
	scalar := func(v any) string {
		switch x := v.(type) {
		case string:
			return strings.TrimSpace(x)
		case float64:
			if x == float64(int64(x)) {
				return strconv.FormatInt(int64(x), 10)
			}
			return strconv.FormatFloat(x, 'f', -1, 64)
		case bool:
			return strconv.FormatBool(x)
		}
		return ""
	}
	for _, k := range keys {
		switch v := raw[k].(type) {
		case []any:
			for _, e := range v {
				if s := scalar(e); s != "" {
					out = append(out, k+" "+s)
				}
			}
		default:
			if s := scalar(v); s != "" {
				out = append(out, k+" "+s)
			}
		}
	}
	return out
}

// shardBy is shardByPath with the tools' declared topics beside the file paths.
func shardBy(older []session.Message, workdir string, topics map[string][]string) []event.ContextShard {
	callPath := map[string][]string{} // tool callID → its topics, to attribute a result to its call
	actions := map[string][]string{}  // topic → ordered tool names, for a deterministic brief
	for _, m := range older {
		for _, p := range m.Parts {
			if p.Kind == session.PartToolCall && p.ToolCall != nil {
				if ts := callTopics(workdir, p.ToolCall.Name, p.ToolCall.Args, topics); len(ts) > 0 {
					callPath[p.ToolCall.CallID] = ts
					for _, t := range ts {
						actions[t] = append(actions[t], p.ToolCall.Name)
					}
				}
			}
		}
	}

	byPath := map[string][]string{}      // path → ordered message IDs
	seen := map[string]map[string]bool{} // path → set(msgID), dedupe
	var order []string                   // first-seen path order, for stable output
	var discussion []string
	add := func(path, id string) {
		if seen[path] == nil {
			seen[path] = map[string]bool{}
		}
		if seen[path][id] {
			return
		}
		if _, ok := byPath[path]; !ok {
			order = append(order, path)
		}
		byPath[path] = append(byPath[path], id)
		seen[path][id] = true
	}

	for _, m := range older {
		if strings.HasPrefix(m.ID, "compaction-") {
			continue
		}
		paths := map[string]bool{}
		for _, p := range m.Parts {
			switch {
			case p.Kind == session.PartToolCall && p.ToolCall != nil:
				for _, t := range callTopics(workdir, p.ToolCall.Name, p.ToolCall.Args, topics) {
					paths[t] = true
				}
			case p.Kind == session.PartToolResult && p.ToolResult != nil:
				for _, t := range callPath[p.ToolResult.CallID] {
					paths[t] = true
				}
			}
		}
		if len(paths) == 0 {
			discussion = append(discussion, m.ID)
			continue
		}
		keys := make([]string, 0, len(paths)) // sort so multi-path messages add deterministically
		for k := range paths {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, path := range keys {
			add(path, m.ID)
		}
	}

	shards := make([]event.ContextShard, 0, len(order)+1)
	for _, path := range order {
		shards = append(shards, event.ContextShard{Topic: path, Brief: actionTrail(actions[path]), MessageIDs: byPath[path]})
	}
	if len(discussion) > 0 {
		shards = append(shards, event.ContextShard{
			Topic: "discussion", Brief: "messages not tied to a specific file", MessageIDs: discussion,
		})
	}
	return shards
}

// actionTrail renders a path's tool activity as a deterministic one-line brief, e.g.
// "read · edit×2 · bash" — distinct tools in first-seen order, with a ×N count when
// repeated. Empty when no tools were recorded.
func actionTrail(names []string) string {
	if len(names) == 0 {
		return ""
	}
	var order []string
	count := map[string]int{}
	for _, n := range names {
		if count[n] == 0 {
			order = append(order, n)
		}
		count[n]++
	}
	parts := make([]string, 0, len(order))
	for _, n := range order {
		if count[n] > 1 {
			parts = append(parts, fmt.Sprintf("%s×%d", n, count[n]))
		} else {
			parts = append(parts, n)
		}
	}
	return strings.Join(parts, " · ")
}

// shardPath extracts the file a tool call targeted and returns it relative to workdir;
// "" when the call references no file (e.g. bash, web tools). It reads "path" (most file
// tools) or "file", so those land on the right topic, not "discussion".
func shardPath(workdir string, args json.RawMessage) string {
	var a struct {
		Path string `json:"path"`
		File string `json:"file"`
	}
	_ = json.Unmarshal(args, &a)
	p := a.Path
	if p == "" {
		p = a.File
	}
	if p == "" {
		return ""
	}
	return relForChange(workdir, p)
}

// truncateAt returns events with seq <= boundary.
// raiseToMessageSeam is snapToMessageSeam's other direction: boundary climbs to the straddling
// message's last part, repeating until no message is split. Used only when lowering reached
// zero — it folds MORE than asked, never less than whole messages.
func raiseToMessageSeam(evs []event.Event, boundary int64) int64 {
	spans := messageSpans(evs)
	for {
		moved := false
		for _, sp := range spans {
			if sp.first <= boundary && boundary < sp.last {
				boundary = sp.last
				moved = true
			}
		}
		if !moved {
			return boundary
		}
	}
}

// snapToMessageSeam lowers boundary until no message's parts straddle it: for any message with
// parts on both sides, the boundary moves to just before that message's first part, repeating
// until stable. Lowering is the safe direction — it folds less, it never loses.
func snapToMessageSeam(evs []event.Event, boundary int64) int64 {
	spans := messageSpans(evs)
	for {
		moved := false
		for _, sp := range spans {
			if sp.first <= boundary && boundary < sp.last {
				boundary = sp.first - 1
				moved = true
			}
		}
		if !moved {
			return boundary
		}
	}
}

// lastCompactionBoundary is the seq the newest compaction already replaced up to — the floor a
// new fold has to beat to reclaim anything at all.
func lastCompactionBoundary(evs []event.Event) int64 {
	var last int64
	for _, e := range evs {
		if e.Type != event.TypeCompaction {
			continue
		}
		var d event.CompactionData
		if json.Unmarshal(e.Data, &d) == nil && d.ReplacesUpToSeq > last {
			last = d.ReplacesUpToSeq
		}
	}
	return last
}

// span is one message's first and last part seq; messageSpans indexes them for the seam walks.
type span struct{ first, last int64 }

func messageSpans(evs []event.Event) map[string]*span {
	spans := map[string]*span{}
	for _, e := range evs {
		if e.Type != event.TypePartAppended {
			continue
		}
		var d event.PartAppendedData
		if json.Unmarshal(e.Data, &d) != nil || d.MessageID == "" {
			continue
		}
		sp, ok := spans[d.MessageID]
		if !ok {
			spans[d.MessageID] = &span{first: e.Seq, last: e.Seq}
			continue
		}
		if e.Seq < sp.first {
			sp.first = e.Seq
		}
		if e.Seq > sp.last {
			sp.last = e.Seq
		}
	}
	return spans
}

func truncateAt(evs []event.Event, boundary int64) []event.Event {
	out := make([]event.Event, 0, len(evs))
	for _, e := range evs {
		if e.Seq <= boundary {
			out = append(out, e)
		}
	}
	return out
}

// flattenForSummary renders a conversation as plain text: every tool call and tool result becomes
// a PartText line, so the summarizer request carries no tool_use structure and needs no Tools
// declared. Roles are preserved; only the shape of each part changes.
func flattenForSummary(msgs []session.Message) []session.Message {
	out := make([]session.Message, 0, len(msgs))
	for _, m := range msgs {
		var text []string
		for _, p := range m.Parts {
			switch {
			case p.Kind == session.PartText && p.Text != "":
				text = append(text, p.Text)
			case p.ToolCall != nil:
				text = append(text, fmt.Sprintf("[called %s %s]", p.ToolCall.Name, string(p.ToolCall.Args)))
			case p.ToolResult != nil:
				text = append(text, fmt.Sprintf("[result: %s]", string(p.ToolResult.Content)))
			}
		}
		joined := strings.TrimSpace(strings.Join(text, "\n"))
		if joined == "" {
			continue
		}
		// The assistant's own turns stay the assistant's; a tool-result message would be an
		// illegal tool role with no tool_call to answer once flattened, so it rides in as user
		// context — which is all a summarizer needs it to be.
		role := m.Role
		if role != session.RoleAssistant {
			role = session.RoleUser
		}
		out = append(out, session.Message{Role: role, Parts: []session.Part{{Kind: session.PartText, Text: joined}}})
	}
	return out
}

// keepTailTokens is how much recent conversation a fold keeps verbatim: CompactKeep of the
// compaction budget. 0 when the window is unknown — then the event-count floor is all there is.
func (a *App) keepTailTokens(s session.Session) int {
	window := a.contextWindow(s.Model.Model)
	if window <= 0 {
		return 0
	}
	return int(float64(window) * a.cfg.CompactRatio * a.cfg.CompactKeep)
}

// briefCapTokens is how large the running brief may grow before it is condensed: a tenth of the
// compaction budget, or a fixed 4k when the window is unknown (an unbounded brief on an unknown
// window is the one case where the accumulation could eat the window it exists to save).
func (a *App) briefCapTokens(s session.Session) int {
	window := a.contextWindow(s.Model.Model)
	if window <= 0 {
		return 4000
	}
	return int(float64(window) * a.cfg.CompactRatio * briefShare)
}

const briefShare = 0.1

// tailBoundary is the seq below which everything is folded so that what remains fits keepTokens:
// messages are walked from the newest backwards, and the first one that does not fit is folded
// whole (its last seq is the boundary). 0 when everything fits — then the caller's own floor
// decides — or when the message that overflows has no seq of its own (a brief).
func tailBoundary(evs []event.Event, keepTokens int) int64 {
	last := map[string]int64{}
	for _, e := range evs {
		var id string
		switch e.Type {
		case event.TypePartAppended:
			var d event.PartAppendedData
			if json.Unmarshal(e.Data, &d) == nil {
				id = d.MessageID
			}
		case event.TypePromptSubmitted:
			var d event.PromptSubmittedData
			if json.Unmarshal(e.Data, &d) == nil {
				id = d.MessageID
			}
		}
		if id != "" && e.Seq > last[id] {
			last[id] = e.Seq
		}
	}
	msgs := reconstruct(evs)
	total := 0
	for i := len(msgs) - 1; i >= 0; i-- {
		total += estimateTokens("", msgs[i:i+1])
		if total <= keepTokens {
			continue
		}
		return last[msgs[i].ID]
	}
	return 0
}

// lastCompactionSeq is the seq of the newest compaction event itself (not the boundary it wrote).
func lastCompactionSeq(evs []event.Event) int64 {
	var out int64
	for _, e := range evs {
		if e.Type == event.TypeCompaction && e.Seq > out {
			out = e.Seq
		}
	}
	return out
}

// priorSummary is the brief the last fold at or below boundary wrote — what the next brief is
// appended to. "" when no fold is being folded over.
func priorSummary(evs []event.Event, boundary int64) string {
	out := ""
	for _, e := range evs {
		if e.Type != event.TypeCompaction || e.Seq > boundary {
			continue
		}
		var d event.CompactionData
		if json.Unmarshal(e.Data, &d) == nil {
			out = d.Summary
		}
	}
	return out
}

// withoutBriefs drops earlier folds' briefs from what the summariser reads: they are kept
// verbatim by the caller, and re-summarising them is what made briefs drift.
func withoutBriefs(msgs []session.Message) []session.Message {
	out := make([]session.Message, 0, len(msgs))
	for _, m := range msgs {
		if strings.HasPrefix(m.ID, "compaction-") {
			continue
		}
		out = append(out, m)
	}
	return out
}

// foldPrompt is what the summariser is asked for. It names the sections because a one-line "keep
// the key facts" produced briefs that kept the narrative and lost the identifiers — the cell
// range, the slide number, the file the user named — which are exactly what the agent needs to
// go on. And it says what a brief is NOT: the current state of anything. A fold's brief describes
// what was done; the state is in the file, and an agent that trusted a brief's "B6 holds 58150"
// after somebody edited B6 acted on a number that no longer existed.
const foldPrompt = `You are folding the older part of a long working conversation into a brief that will REPLACE it. The agent continues from this brief plus the most recent turns, so anything not written here is gone.

Write these sections, in this order, with these headings:
1. Request — what the user asked for (their own words where the wording matters) and every constraint or preference they stated.
2. Decisions — what was decided and why, including what was considered and ruled out.
3. Done — what was changed or produced, naming exactly what was touched (files, sheets, cell ranges, slides, paragraphs, ids, commands) and the outcome of each.
4. Open — what remains, what was tried and failed (so it is not tried again the same way), and anything the user is still waiting on.
5. Names — identifiers, paths, numbers and terms the user uses that must be repeated exactly.

Keep identifiers, paths, code and numbers verbatim. Describe what was DONE to a file or document, never its current contents as fact — the state is re-read from the source when needed. Do not record personal data — email addresses, phone numbers, account or card numbers — unless the task itself is about them. Be concise; omit pleasantries and narration. Write only the brief.`

// condensePrompt rewrites an accumulated brief once it outgrows its share of the window.
const condensePrompt = `Condense the following brief of a long working conversation into a shorter one with the same five sections (Request, Decisions, Done, Open, Names). Merge repeated points, drop narration, keep every identifier, path, number and unresolved item verbatim. Write only the condensed brief.`

// summarizeViaLLM asks the model to fold prior conversation into a brief that preserves the
// request, the decisions, what was done, what is open and the names in play. It uses the agent's
// own provider so compaction runs on the same backend the agent is routed to, and asks for the
// brief in the language the user was writing in.
func (a *App) summarizeViaLLM(ctx context.Context, agent AgentSpec, s session.Session, msgs []session.Message) string {
	system := foldPrompt
	if d := langDirective(personsWords(msgs)); d != "" {
		system += "\n\n" + d
	}
	return a.askForBrief(ctx, agent, s, system, asMaterial(flattenForSummary(msgs)))
}

// asMaterial hands the conversation to the summariser as ONE user message quoting it, role by
// role, with the instruction last. Sent as a message list it was a conversation to CONTINUE: a
// fold over a short tail ending in a tool result came back as the assistant's next reply ("no new
// request, nothing further to do" — measured 2026-09-07), and that sentence became the session's
// memory of everything it replaced. Quoted, it is material, and the last line says what to do.
func asMaterial(msgs []session.Message) []session.Message {
	var b strings.Builder
	b.WriteString("The conversation to fold follows, oldest first, each turn marked by its role.\n\n<conversation>\n")
	for _, m := range msgs {
		for _, p := range m.Parts {
			if p.Kind == session.PartText && p.Text != "" {
				b.WriteString("[")
				b.WriteString(string(m.Role))
				b.WriteString("]\n")
				b.WriteString(p.Text)
				b.WriteString("\n\n")
			}
		}
	}
	b.WriteString("</conversation>\n\nWrite the brief now — do not answer or continue the conversation.")
	return []session.Message{{Role: session.RoleUser, Parts: []session.Part{{Kind: session.PartText, Text: b.String()}}}}
}

// condenseBrief rewrites the running brief, shorter. "" when the model gave nothing — the caller
// keeps the long one rather than losing it.
func (a *App) condenseBrief(ctx context.Context, agent AgentSpec, s session.Session, brief string) string {
	system := condensePrompt
	if d := langDirective(brief); d != "" {
		system += "\n\n" + d
	}
	return a.askForBrief(ctx, agent, s, system,
		[]session.Message{{Role: session.RoleUser, Parts: []session.Part{{Kind: session.PartText, Text: brief}}}})
}

// personsWords is what the person wrote in these messages, joined — the sample the language of the
// brief is chosen from.
func personsWords(msgs []session.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		if m.Role != session.RoleUser {
			continue
		}
		for _, p := range m.Parts {
			if p.Kind == session.PartText {
				b.WriteString(p.Text)
				b.WriteString("\n")
			}
		}
	}
	return b.String()
}

// askForBrief is the one model call both briefs go through: the session's provider, the
// configured summariser model when there is one, and the truncation marking every brief needs.
func (a *App) askForBrief(ctx context.Context, agent AgentSpec, s session.Session, system string, msgs []session.Message) string {
	prov := a.providerFor(agent)
	if prov == nil {
		// The same guard generate_step grew: a reader-only App has no backend, and dereferencing
		// nil here took the process down. Nothing to summarize on is a failure to report, not a
		// crash.
		a.emitToolProgress(s.ID, event.Actor{Kind: event.ActorSystem, ID: "compact"}, "", "compact",
			"compact: no model backend to summarize with, so this fold was skipped")
		return ""
	}
	model := s.Model.Model
	if a.cfg.CompactModel != "" {
		model = a.cfg.CompactModel
	}
	req := port.ChatRequest{
		Model:  model,
		System: system,
		// Tool calls and results flattened to plain text (the caller did it). The request carries
		// no Tools, and a strict backend (Anthropic via a gateway) rejects a message holding
		// tool_use blocks with no tools declared — so every auto-fold on such a route failed
		// silently, forever. The summarizer only needs to READ the conversation; rendered as prose
		// it says the same thing and goes out legally on any backend.
		Messages: msgs,
	}
	stream, err := prov.StreamChat(ctx, req)
	if err != nil {
		// A provider error here is not "nothing to fold" — which is what returning "" tells the
		// caller. Left silent, the caller re-tried the same doomed call every step and the turn
		// died with a raw overflow no one could connect to a failing compaction. Reported, and
		// still returns "" so the caller keeps the history rather than replacing it with nothing.
		a.emitToolProgress(s.ID, event.Actor{Kind: event.ActorSystem, ID: "compact"}, "", "compact",
			fmt.Sprintf("compact: the summarizer was refused (%v) — history kept, not folded", err))
		return ""
	}
	text, cut := drainStream(stream)
	if cut != nil {
		// This summary BECOMES the session's memory of everything it replaces, so a half-written
		// one silently discards conversation. Report it; the caller still uses what arrived rather
		// than losing the region entirely.
		a.emitToolProgress(s.ID, event.Actor{Kind: event.ActorSystem, ID: "compact"}, "", "compact",
			fmt.Sprintf("compact: the summary was CUT OFF after %d chars — %v (the compacted region is summarized incompletely)", len(text), cut))
		// …and say it INSIDE the summary. The line above goes to the operator; the reader who
		// depends on this text is the agent, and for it this paragraph is now the whole of what
		// happened before. Every other truncation in magi marks itself in the artifact it cut —
		// the result cap, the capture head/tail, the evidence block's dropped tail — because a
		// reader cannot ask for what it does not know is missing. This one only told the console.
		text = strings.TrimSpace(text)
		if text != "" {
			text += "\n\n[this summary is INCOMPLETE — the model was cut off while writing it, so " +
				"parts of what it replaces are described nowhere. Treat gaps as unknown rather than " +
				"as nothing having happened; recall_context can re-open the detail by topic.]"
		}
		return text
	}
	return strings.TrimSpace(text)
}

// contextTokens reports the context size: the provider's real prompt_tokens from
// the last turn when available, otherwise a chars/4 estimate. Real counts make
// the meter and compaction trigger accurate.
func (a *App) contextTokens(sid session.SessionID, sys string, msgs []session.Message) int {
	if n := a.realPromptTokens(sid); n > 0 {
		return n
	}
	return estimateTokens(sys, msgs)
}

// estimateTokens approximates the token count of a request (≈4 chars/token).
//
// It must count what the WIRE carries, not what the session holds. Reasoning parts are persisted
// every step and rebuilt on replay, but the openai adapter's joinText sends only PartText — so
// counting a reasoning part's Text here inflated the estimate by the bulk of a thinking model's
// output (routinely several times the text), and the trigger takes max(real, estimate). The result
// was a session folded away at 15-20% of real window use, and re-folded on later turns for the
// same phantom reason. Count PartText, tool calls and tool results; skip the reasoning that never
// leaves the machine.
// toolSpecTokens approximates what the request's tool definitions cost on the wire (≈4 chars/token):
// every name, description and schema travels with EVERY request, ahead of the messages.
func toolSpecTokens(specs []port.ToolSpec) int {
	chars := 0
	for _, t := range specs {
		chars += len(t.Name) + len(t.Description) + len(t.Schema)
	}
	return chars / 4
}

func estimateTokens(sys string, msgs []session.Message) int {
	chars := len(sys)
	for _, m := range msgs {
		for _, p := range m.Parts {
			if p.Kind == session.PartText {
				chars += len(p.Text)
			}
			if p.ToolCall != nil {
				chars += len(p.ToolCall.Name) + len(p.ToolCall.Args)
			}
			if p.ToolResult != nil {
				chars += len(p.ToolResult.Content)
			}
		}
	}
	return chars / 4
}
