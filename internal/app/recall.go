package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// recallRenderCap bounds a single recall's output, well below capToolResult (~64KB),
// so re-hydrating a large shard can't blow the window back open in one shot.
const recallRenderCap = 24 << 10

// recallContext re-hydrates a topic that an earlier compaction shed from the live
// context. It gathers the shards from every compaction in the session, matches the
// query against their topics, and rebuilds the matched shard's ORIGINAL messages from
// the (always-persisted) event log — verbatim, not the lossy summary. On no/ambiguous
// match it returns the list of recoverable topics so the model can pick. The result is
// a normal tool result (never an error), so the agent can react to it.
func (a *App) recallContext(ctx context.Context, sid session.SessionID, query string, guard *runGuard) (string, error) {
	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		return "", err
	}
	var shards []event.ContextShard
	for _, e := range evs {
		if e.Type != event.TypeCompaction {
			continue
		}
		var d event.CompactionData
		if json.Unmarshal(e.Data, &d) == nil {
			shards = append(shards, d.Shards...)
		}
	}
	if len(shards) == 0 {
		return "Nothing has been compacted yet, so there is no earlier context to recall.", nil
	}
	// One compaction indexes a topic once, but the SAME topic (a re-edited file, or
	// "discussion") recurs across compactions — merge by topic so recall reaches every
	// region's detail, not just the earliest.
	shards = mergeShardsByTopic(shards)

	idx, ambiguous := matchShard(query, shards)
	if idx < 0 || ambiguous {
		lead := fmt.Sprintf("No topic matched %q. ", strings.TrimSpace(query))
		if ambiguous {
			lead = fmt.Sprintf("%q matched more than one topic. ", strings.TrimSpace(query))
		}
		return lead + "Recoverable topics: " + strings.Join(shardTopics(shards), "; "), nil
	}

	sh := shards[idx]
	// Charge the per-turn budget only now that a topic actually resolved (a miss costs
	// nothing), keyed on the matched topic so two phrasings of the same topic don't each
	// re-inflate context.
	if guard != nil {
		if ok, why := guard.allowRecall(sh.Topic); !ok {
			return why, nil
		}
	}
	msgs := rebuildMessages(evs, sh.MessageIDs)
	if len(msgs) == 0 {
		return fmt.Sprintf("Topic %q matched but its messages could not be rebuilt.", sh.Topic), nil
	}
	return renderRecall(sh.Topic, sh.Brief, msgs), nil
}

// mergeShardsByTopic coalesces shards that share a topic (the same file re-touched in a
// later compacted region, or the recurring "discussion" bucket) into one, concatenating
// their message IDs in first-seen order. Regions are disjoint, so the IDs never collide.
func mergeShardsByTopic(shards []event.ContextShard) []event.ContextShard {
	idx := map[string]int{}
	out := make([]event.ContextShard, 0, len(shards))
	for _, sh := range shards {
		if i, ok := idx[sh.Topic]; ok {
			out[i].MessageIDs = append(out[i].MessageIDs, sh.MessageIDs...)
			if out[i].Brief == "" {
				out[i].Brief = sh.Brief
			}
			continue
		}
		idx[sh.Topic] = len(out)
		cp := sh
		cp.MessageIDs = append([]string(nil), sh.MessageIDs...)
		out = append(out, cp)
	}
	return out
}

// matchShard picks the shard best matching query. Tiers, strongest first: exact topic,
// basename, substring either way, then token overlap on topic+brief. Returns ambiguous
// when the top score is only a weak (token-overlap) tie, so the caller lists topics
// instead of guessing. idx is -1 when nothing matches at all.
func matchShard(query string, shards []event.ContextShard) (idx int, ambiguous bool) {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return -1, false
	}
	best, second, bestIdx := 0, 0, -1
	for i, sh := range shards {
		topic := strings.ToLower(sh.Topic)
		score := 0
		switch {
		case topic == q:
			score = 1000
		case baseName(topic) == q:
			score = 900
		case strings.Contains(topic, q):
			score = 500 + len(q)
		case strings.Contains(q, topic) && looksLikePath(topic):
			// Only a path-shaped topic may match by being a substring of the query;
			// a generic word ("discussion") must not shadow the intended file.
			score = 400
		default:
			score = 10 * tokenOverlap(q, topic+" "+strings.ToLower(sh.Brief))
		}
		if score > best {
			best, second, bestIdx = score, best, i
		} else if score > second {
			second = score
		}
	}
	if best == 0 {
		return -1, false
	}
	// A top score that ties with the runner-up (two files both substring-matching, two
	// same-basename paths, equal token overlap) is too uncertain to act on — let the
	// caller surface the topic list instead of guessing.
	if best == second {
		return bestIdx, true
	}
	return bestIdx, false
}

// looksLikePath reports whether a topic is file-path-shaped (has a separator or
// extension) rather than a generic label like "discussion".
func looksLikePath(topic string) bool {
	return strings.ContainsAny(topic, "/.")
}

// renderRecall formats a shard's messages, keeping the MOST RECENT within recallRenderCap
// (recent detail is the likeliest reason to recall) and noting any older trim.
func renderRecall(topic, brief string, msgs []session.Message) string {
	rendered := make([]string, len(msgs))
	for i, m := range msgs {
		rendered[i] = renderRecalledMessage(m)
	}
	// Take from the end until the cap, then restore chronological order.
	total, start := 0, 0
	for i := len(rendered) - 1; i >= 0; i-- {
		total += len(rendered[i]) + 1
		if total > recallRenderCap {
			start = i + 1
			break
		}
	}
	if start >= len(rendered) { // even the newest message alone exceeds the cap → still emit it
		start = len(rendered) - 1
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[recalled topic: %s — %d message(s)", topic, len(msgs))
	if start > 0 {
		fmt.Fprintf(&b, ", showing the latest %d (older trimmed to fit; narrow the topic for the rest)", len(msgs)-start)
	}
	b.WriteString("]")
	if brief != "" {
		b.WriteString("\n" + brief)
	}
	b.WriteString("\n\n")
	b.WriteString(strings.Join(rendered[start:], "\n"))
	return b.String()
}

func renderRecalledMessage(m session.Message) string {
	var b strings.Builder
	b.WriteString(string(m.Role) + ":")
	for _, p := range m.Parts {
		switch p.Kind {
		case session.PartText:
			if t := strings.TrimSpace(p.Text); t != "" {
				b.WriteString(" " + t)
			}
		case session.PartToolCall:
			if p.ToolCall != nil {
				fmt.Fprintf(&b, "\n  ⚙ %s %s", p.ToolCall.Name, string(p.ToolCall.Args))
			}
		case session.PartToolResult:
			if p.ToolResult != nil {
				fmt.Fprintf(&b, "\n  → %s", string(p.ToolResult.Content))
			}
		}
	}
	return b.String()
}

func shardTopics(shards []event.ContextShard) []string {
	seen := map[string]bool{}
	var out []string
	for _, sh := range shards {
		if !seen[sh.Topic] {
			seen[sh.Topic] = true
			out = append(out, sh.Topic)
		}
	}
	sort.Strings(out)
	return out
}

func baseName(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

// tokenOverlap counts how many alphanumeric tokens of a appear in b.
func tokenOverlap(a, b string) int {
	split := func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	}
	n := 0
	for _, tok := range strings.FieldsFunc(a, split) {
		if len(tok) >= 2 && strings.Contains(b, tok) {
			n++
		}
	}
	return n
}
