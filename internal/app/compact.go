package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
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
	lastResult := "" // callID of the newest result — exempt
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
		saved += c.bytes / 4
		n++
		if saved >= overTokens {
			break
		}
	}
	if n > 0 {
		a.emitToolProgress(s.ID, actor, "", "compact",
			fmt.Sprintf("freed the window by eliding %d bulky tool result(s) — re-derivable, and already narrated", n))
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
	shards := shardByPath(older, s.Workdir)
	// Say it is happening. Compaction is a model call on a large prompt — measured in tens of
	// seconds on a local backend — and it runs BETWEEN steps, so nothing else is being drawn: the
	// transcript simply stops. From the outside that is indistinguishable from a wedged turn, and
	// it is the one long pause on this page that had no line to explain it. Tool calls have said
	// what they are doing all along; this rides the same channel, which is also the only one a
	// browser in another process can read (noteDoing).
	a.emitToolProgress(s.ID, actor, "", "compact",
		fmt.Sprintf("compacting the conversation — summarising %d earlier messages", len(older)))
	summary := a.summarizeViaLLM(ctx, agent, s, older)
	if summary == "" {
		a.emitToolProgress(s.ID, actor, "", "compact", "compaction did not produce a summary — keeping the conversation as it is")
		return false
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
	callPath := map[string]string{}  // tool callID → relative path, to attribute a result to its call's file
	actions := map[string][]string{} // path → ordered tool names, for a deterministic brief
	for _, m := range older {
		for _, p := range m.Parts {
			if p.Kind == session.PartToolCall && p.ToolCall != nil {
				if rel := shardPath(workdir, p.ToolCall.Args); rel != "" {
					callPath[p.ToolCall.CallID] = rel
					actions[rel] = append(actions[rel], p.ToolCall.Name)
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
				if rel := shardPath(workdir, p.ToolCall.Args); rel != "" {
					paths[rel] = true
				}
			case p.Kind == session.PartToolResult && p.ToolResult != nil:
				if rel := callPath[p.ToolResult.CallID]; rel != "" {
					paths[rel] = true
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

// summarizeViaLLM asks the model to summarize prior conversation into a compact
// brief that preserves decisions, facts, and open tasks. It uses the agent's own
// provider so compaction runs on the same backend the agent is routed to.
func (a *App) summarizeViaLLM(ctx context.Context, agent AgentSpec, s session.Session, msgs []session.Message) string {
	prov := a.providerFor(agent)
	if prov == nil {
		// The same guard generate_step grew: a reader-only App has no backend, and dereferencing
		// nil here took the process down. Nothing to summarize on is a failure to report, not a
		// crash.
		a.emitToolProgress(s.ID, event.Actor{Kind: event.ActorSystem, ID: "compact"}, "", "compact",
			"compact: no model backend to summarize with, so this fold was skipped")
		return ""
	}
	req := port.ChatRequest{
		Model: s.Model.Model,
		System: "Summarize the following conversation into a concise brief that preserves key facts, " +
			"decisions, file changes, and any unfinished tasks. Write only the summary.",
		// Tool calls and results flattened to plain text. The request carries no Tools, and a strict
		// backend (Anthropic via a gateway) rejects a message holding tool_use blocks with no tools
		// declared — so every auto-fold on such a route failed silently, forever. The summarizer
		// only needs to READ the conversation; rendered as prose it says the same thing and goes out
		// legally on any backend.
		Messages: flattenForSummary(msgs),
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
