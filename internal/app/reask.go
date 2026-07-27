package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/jsonx"
)

// Several passes here are the same conversation with a model: ask for JSON, and when the reply
// cannot be used, ask ONCE more naming what was wrong with the first. The planner, the check audit,
// the coverage fill, the curator, and the spec-mine distill all do it, and each one had written the
// whole exchange out by hand.
//
// Hand-written copies of a thing do not stay equal. This one had already drifted in a way with a
// cost: only the planner PERSISTED the reply it could not read, and its reason for doing so — the
// progress log is bus-only and whitespace-collapsed, so it destroys exactly the control characters
// that most often cause the failure — is not about plans at all. It is true of every reply on this
// list. The other four failed twice and left a collapsed excerpt, which is not enough to tell a
// truncated reply from a raw newline inside a string.
//
// So the shared part is the exchange and its record: the two log lines, the re-ask, and the
// diagnostic fact for each unusable reply. What stays with each caller is what genuinely differs —
// how a reply is parsed, what counts as unusable, which reminder names that defect, and what
// happens when the second reply is unusable too.

// reask is one such exchange. The first ask has already happened; this describes how to judge it,
// how to ask again, and how the pass is named in the record.
type reask[T any] struct {
	pass  string      // names this pass in the log and in the diagnostic record ("check-audit")
	actor event.Actor // who the progress line is attributed to

	// label distinguishes several occasions of the same pass, when there are any. Empty for passes
	// that happen once. The planner runs the same exchange as a first plan, as a council revision,
	// and as a decompose, and a log that called all three "planner" would hide the one thing worth
	// seeing — a re-plan loop looks like a loop only if its rounds are named.
	label string

	// ask runs the pass again with a system prompt that now carries a reminder, and returns what it
	// got: the parsed value, the raw reply, and whether the reply had the shape this pass reads.
	//
	// It is ONE knob rather than a call/parse pair because for some passes the two cannot be pulled
	// apart — the coverage fill's answer is not a value until it has been merged with the authored
	// checks and re-scored for coverage, and splitting that would mean handing the merge a reply it
	// had to re-derive. Callers own the transport too, since one of these streams with a stall
	// watchdog and the rest are plain side calls.
	ask func(system string) (T, string, bool)

	// defect returns "" when a value is usable, and otherwise a phrase naming what is wrong with it,
	// which becomes the log line. Parsing is not the only way a reply is unusable — an empty brief,
	// an audit that drops every check, a fill that covers nothing new all parse perfectly and are
	// still nothing to go on.
	defect func(v T, ok bool, raw string) string

	// reminder builds the system prompt for the second ask. It takes the first reply and whether it
	// parsed, because the failure shapes are different defects and a reminder that names the wrong
	// one asks the model to fix something it did not do.
	reminder func(raw string, ok bool) string

	// probe reports why a reply could not be read, for the diagnostic record's Kind. Each pass
	// unmarshals into its own type, and the type is what turns "valid JSON" into "valid JSON of the
	// wrong shape". Nil probes into a bare any, which sees syntax only.
	probe func([]byte) error

	// keep decides whether a usable second reply replaces the first. Nil means it always does, which
	// is right whenever the first was unusable outright. It exists for the one caller whose first
	// reply is partly usable, where a worse second answer must not displace it.
	keep func(retry, first T) bool

	// fallback names what the caller does when the second reply is unusable too. It is the last line
	// this pass writes, so it has to say what the run will now proceed with.
	fallback string
}

// run judges the first reply and, if it cannot be used, asks once more. It returns the value to use
// and whether that value is usable — never more than two calls, never a loop.
func (r reask[T]) run(ctx context.Context, a *App, sid session.SessionID, first T, firstRaw string, firstOK bool) (T, bool) {
	bad := r.defect(first, firstOK, firstRaw)
	if bad == "" {
		return first, true
	}
	name := r.pass
	if r.label != "" {
		name = r.label
	}
	a.emitToolProgress(sid, r.actor, "", r.pass,
		fmt.Sprintf("%s: %s — re-asking once :: %s", name, bad, jsonx.Report(firstRaw)))
	a.recordParseFailure(ctx, sid, r.pass, r.label, firstRaw, r.probe)

	v2, raw2, ok2 := r.ask(r.reminder(firstRaw, firstOK))
	if bad2 := r.defect(v2, ok2, raw2); bad2 != "" {
		a.emitToolProgress(sid, r.actor, "", r.pass,
			fmt.Sprintf("%s: the re-ask was unusable too (%s) — %s :: %s",
				name, bad2, r.fallback, jsonx.Report(raw2)))
		a.recordParseFailure(ctx, sid, r.pass, strings.TrimPrefix(r.label+"-retry", "-"), raw2, r.probe)
		return first, false
	}
	if r.keep != nil && !r.keep(v2, first) {
		a.emitToolProgress(sid, r.actor, "", r.pass,
			fmt.Sprintf("%s: the re-ask did not improve on what was salvaged — keeping the first reply :: %s",
				name, jsonx.Report(raw2)))
		return first, true
	}
	return v2, true
}

// recordParseFailure persists a reply that could not be used, as a diagnostic fact.
//
// It exists because the progress line is not a record: it goes to the bus, it is clipped, and its
// whitespace is collapsed — which erases the raw newline inside a string value that is one of the
// most common ways a weak model's JSON fails. Without the reply itself, an intermittent failure can
// only be guessed at afterwards.
//
// Best-effort and bounded: the raw is clipped, and a failed write is a no-op. detail keeps the text
// at full fidelity (NOT collapsed) up to the clip, which is the whole point.
func (a *App) recordParseFailure(ctx context.Context, sid session.SessionID, pass, tag, text string, probe func([]byte) error) {
	const clip = 8192
	if strings.TrimSpace(text) == "" {
		// Nothing came back at all, which is a transport failure, not a reply that could not be read.
		// Filing it as a parse failure would put an empty record in the pile a later reader groups by
		// cause, and every one of those is a wrong lead.
		return
	}
	raw := text
	if len(raw) > clip {
		raw = raw[:clip] + fmt.Sprintf("\n…[%d more chars]", len(text)-clip)
	}
	source := pass
	if tag != "" {
		source += ":" + tag
	}
	d, _ := json.Marshal(event.DiagnosticData{
		Source: source,
		Kind:   parseFailureKind(text, probe),
		Detail: raw,
	})
	_ = a.appendFact(ctx, sid, event.TypeDiagnostic, plannerActor, d)
}

// parseFailureKind classifies WHY a reply could not be read, as a short token the record can be
// grouped by. It re-walks the reply the way a lenient reader would and reports the first
// discriminating cause: no JSON at all; a bracket that never closes (the output budget cut the
// reply off); a control character inside a string value (what a multi-line value produces, and the
// case repairs cannot fix); some other invalid syntax; or well-formed JSON that simply did not
// carry what the pass asked for.
//
// Arrays count, not just objects: two of these passes ask for a bare array, and a classifier that
// only looked for `{` would report every one of their replies as prose.
//
// probe is the caller's own unmarshal, and it is what separates "valid JSON" from "valid JSON of
// the wrong shape" — a schema mismatch is invisible to a probe into a bare any.
func parseFailureKind(text string, probe func([]byte) error) string {
	if probe == nil {
		probe = func(b []byte) error { var v any; return json.Unmarshal(b, &v) }
	}
	if strings.IndexByte(text, '{') < 0 && strings.IndexByte(text, '[') < 0 {
		return "no-json" // pure prose, or the JSON never started
	}
	// The INTACT spans, not the recovered ones: this names why the reply was damaged, and a
	// classifier that reported a truncated reply as merely "empty" would describe the repair instead
	// of the defect — the reader recovers what it can, the diagnosis still says what went wrong.
	spans := append(jsonx.BalancedObjects(text), jsonx.BalancedArrays(text)...)
	if len(spans) == 0 {
		return "unbalanced-or-truncated" // a bracket opened and never closed
	}
	var firstErr error
	for _, js := range spans {
		err := probe([]byte(js))
		if err == nil {
			continue // valid, but (on this path) carried nothing usable — keep looking for the real defect
		}
		// A span of the OTHER kind is not a defect in the reply, it is a span this pass does not read:
		// scanning for both objects and arrays means a plan's `"steps":[]` is offered to a probe that
		// wants an object, and an array-reading pass is offered each element. A top-level kind mismatch
		// (an UnmarshalTypeError naming no field) is exactly that case, and reporting it would blame
		// the reply for the scan's breadth. A mismatch INSIDE a field is a real schema defect and stays.
		var te *json.UnmarshalTypeError
		if errors.As(err, &te) && te.Field == "" {
			continue
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	if firstErr == nil {
		return "parsed-but-empty" // every span read; none carried what the pass asked for
	}
	msg := firstErr.Error()
	switch {
	case strings.Contains(msg, "in string literal"):
		return "control-char-in-string" // a raw newline/tab/control char inside a value
	case strings.Contains(msg, "invalid character"):
		return "invalid-json-syntax"
	default:
		return "unmarshal-error"
	}
}
