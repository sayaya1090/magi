package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// streamStep is the outcome of consuming one model-response stream: the finalized
// assistant text/reasoning, the tool calls it requested, and the usage report.
type streamStep struct {
	text          string
	reasoning     string
	toolCalls     []*session.ToolCall
	usage         *event.Usage
	textConsumed  bool // the text was a prompt-fallback tool call, not a real answer
	reasoningSpun bool // the response was cancelled as a reasoning-only spin (see reasoningSpinCap)
	stalled       bool // the stream went silent before the FIRST token (hung backend) — safe to retry
	// cut is set when the stream carried a reply and then ended with neither finish_reason nor
	// [DONE]. The text is a prefix; the loop says so and keeps going (see cutByLostStreamNote).
	cut bool
	// finishReason is the provider's finish_reason for this response, unread. "length" means the
	// output-token cap ended it, not the model — see cutByOutputCapNote.
	finishReason string
}

// cutByOutputCapNote is injected after a response the provider ended at the output-token cap
// (finish_reason "length"). Nothing else marks it: the stream closes normally, the text and any
// tool calls persist, and a reply that stops mid-sentence — or mid-argument, which is how a tool
// call is lost to repairArgs — is indistinguishable from one the model chose to end.
//
// It states the measured fact and what follows from it mechanically. Whether anything important
// was cut is the model's read, not magi's.
const cutByOutputCapNote = "Your last reply did not end because you finished it: the provider " +
	"stopped it at the output-token cap (finish_reason \"length\"). What was recorded is a PREFIX. " +
	"Anything after the cut — the rest of a sentence, the rest of a tool call's arguments, a tool " +
	"call you had not started — never arrived, and a tool call whose arguments were cut mid-way " +
	"will have failed to parse. Continue from where it stopped, in smaller pieces."

// cutBeforeActingNote replaces it when the cut reply carried NO tool call. The fact is the same;
// what follows from it is not. "Continue in smaller pieces" is advice to keep writing, and a
// response that spent its whole budget without acting does not need more writing — it needs an
// action. Measured in an external run of the same model (extract-elf and large-scale-text-editing,
// 2026-07-31): both turns reasoned into the cap step after step, never called a tool, never
// reached the council, and landed unverified with the deliverable never written.
const cutBeforeActingNote = "Your last reply hit the output-token cap (finish_reason \"length\") " +
	"before it made a single tool call — the whole budget went into text and nothing was done. " +
	"Writing more will hit the same wall. Take the concrete next step with a TOOL now: write the " +
	"file, run the command, or report. Do not re-derive what you were working out; act on it."

// cutByLostStreamNote is injected after a reply the CONNECTION ended rather than the model. The
// recorded text is a prefix, exactly as at the output-token cap, so the note states the same
// mechanical consequence — and names the different cause, because the two call for different
// reads: a budget cut means the reply was too long, a lost stream means nothing about its length.
//
// It does not tell the model to start over. The prefix is on the record and the model can see how
// far it got; re-deriving from the top is the expensive reading of "your reply was cut", and the
// one a weak model reaches for by default.
const cutByLostStreamNote = "Your last reply did not end because you finished it: the connection " +
	"to the model ended mid-reply, with no finish_reason. What was recorded is a PREFIX — any " +
	"sentence, and any tool call, that was still being written is missing. Nothing about the task " +
	"changed and nothing you did was lost. Continue from where it stopped rather than starting over."

// streamStallTimeout bounds how long a response stream may stay SILENT — no event of any kind — before
// consumeStream aborts it. A hung or wedged backend accepts the request, returns 200, then streams
// nothing; without this the read blocks on the turn's ctx, which for a main generate is the whole
// task wall clock (a 45-minute silent hang observed on a stuck local backend, cobol-modernization).
// It is INACTIVITY-based (reset by every event), so a slow-but-alive model that streams tokens or
// reasoning is never tripped — only a truly dead stream is. A stall BEFORE the first token is
// retryable (streamStep.stalled); a freeze mid-generation just ends the stream with the partial
// output. Var, not const, so tests can shrink it. MAGI_STREAM_STALL overrides (0 disables).
var streamStallTimeout = func() time.Duration {
	if v := strings.TrimSpace(os.Getenv("MAGI_STREAM_STALL")); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return time.Duration(n) * time.Second
		}
	}
	return 120 * time.Second
}()

// maxStreamStallRetries bounds how many times a main generate re-issues a request that stalled before
// its first token, before surfacing the hang as an error rather than retrying forever.
var maxStreamStallRetries = 2

// maxCtxCompactRetries bounds how many times a single generate step force-compacts and re-issues after
// the provider rejects the request as too long. Each retry folds more history into a summary; if the
// context still overflows after this many folds (or nothing is left to fold), the error surfaces
// rather than looping.
var maxCtxCompactRetries = 3

// drainStream accumulates a text-only provider stream and reports whether it was CUT OFF midway.
// Every side call in this package used to drop the error event on the floor and return whatever had
// arrived, so a broken stream was indistinguishable from a complete reply — a half-written summary
// became the session's compacted memory, a half-written verdict killed a working subagent, and a
// half-written document was reported as the model writing bad JSON. The partial text is still
// returned, because salvage wants it; the caller decides what a cut means for its own use.
func drainStream(stream <-chan port.ProviderEvent) (string, error) {
	var b strings.Builder
	var cut error
	for ev := range stream {
		switch ev.Type {
		case port.ProviderText:
			b.WriteString(ev.Text)
		case port.ProviderError:
			cut = ev.Err
		}
	}
	return b.String(), cut
}

// reasoningSpinCap is the max output bytes a single model response may stream WITHOUT ever emitting
// a tool call before it is cancelled as a reasoning-only spin: a weak model that "thinks" forever
// and never acts (observed at 35–70 MB of reasoning deltas with zero tool calls on hard tasks —
// path-tracing, circuit-fibsqrt, qemu). The step/stall guards fire BETWEEN steps, so a response
// that never finishes is invisible to them; this bounds it mid-stream. The default is high enough
// that any legitimate single-response reasoning fits; MAGI_SPIN_CAP overrides (0 disables).
func reasoningSpinCap() int {
	if v := strings.TrimSpace(os.Getenv("MAGI_SPIN_CAP")); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n // 0 = disabled
		}
	}
	return 400_000 // ~400KB of pure output; the observed spins were 175x this
}

// reasoningSpinNudge is injected after a reasoning-only spin is cancelled: stop thinking, act.
const reasoningSpinNudge = "You streamed a very long chain of reasoning without taking ANY action — " +
	"no tool call at all. Thinking alone does not make progress. STOP reasoning now and take the " +
	"concrete next step with a TOOL: write a file, run a command, or report. Do not re-derive what " +
	"you were working out in your head — act."

// consumeStream drains one provider stream, publishing text/reasoning deltas as
// transient events and recording the real prompt-token count for the meter. A
// non-nil error means the provider reported one (already emitted to the bus) and
// the turn must unwind.
// streamDiag enables opt-in stderr stream diagnostics (MAGI_STREAM_DIAG), mirroring
// the adapter-side flag so pre-finish stalls and post-finish drains can be traced
// together in one run without affecting normal operation.
var streamDiag = os.Getenv("MAGI_STREAM_DIAG") != ""

func (a *App) consumeStream(ctx context.Context, sid session.SessionID, agentActor event.Actor, stream <-chan port.ProviderEvent, msgID, textPartID, reasonPartID string, cancel context.CancelFunc) (streamStep, error) {
	var text, reasoning strings.Builder
	var res streamStep
	streamErr := false
	// The output cap and the spin guard were treated as the same thing, and they are not. A cap
	// bounds how BIG one response gets; the guard notices there is still no ACTION and tells the
	// model to take one. Deferring to the cap kept the bound and threw away the recovery: the
	// response ends mid-thought at finish_reason "length", carries no tool call, and the next step
	// begins the same way — with nothing having told the model to stop reasoning. Observed twice in
	// one external run (extract-elf and large-scale-text-editing, 2026-07-31): both spent the turn
	// reasoning into the cap, never called a tool, never reached the council, and landed
	// unverified with the deliverable never written.
	spinCap := reasoningSpinCap()
	// Stall watchdog: `last` is the time of the most recent event, so `now - last` is how long the
	// stream has been silent. The ticker lets the select wake even while the backend sends nothing, so
	// a hung stream is aborted at streamStallTimeout instead of stranding the read until the turn's
	// wall clock. Opt-in diagnostics (MAGI_STREAM_DIAG) also log sustained gaps here.
	var (
		idleC    <-chan time.Time
		last     = time.Now()
		diagLast = time.Now()
		finished bool
		finishAt time.Time
	)
	if streamStallTimeout > 0 || streamDiag {
		tick := 15 * time.Second
		if streamStallTimeout > 0 && streamStallTimeout < tick {
			tick = streamStallTimeout
		}
		t := time.NewTicker(tick)
		defer t.Stop()
		idleC = t.C
	}
loop:
	for {
		var ev port.ProviderEvent
		select {
		case e, ok := <-stream:
			if !ok {
				break loop
			}
			ev = e
			last = time.Now()
			// Tokens are arriving: this generation is producing, which is what lets the lease
			// supervisor tell "mid-sentence" from "wedged backend" (see genFresh).
			a.noteGenToken(sid)
		case now := <-idleC:
			gap := now.Sub(last)
			// Silent past the bound → abort so a wedged backend can't hold the turn to the wall clock.
			// No output yet ⇒ mark it retryable (the caller re-issues the request); a mid-generation
			// freeze just ends the stream with whatever partial output already arrived.
			if streamStallTimeout > 0 && gap >= streamStallTimeout {
				if text.Len()+reasoning.Len() == 0 && len(res.toolCalls) == 0 {
					res.stalled = true
				}
				if cancel != nil {
					cancel()
				}
				break loop
			}
			if streamDiag && now.Sub(diagLast) >= 20*time.Second && gap >= 20*time.Second {
				fmt.Fprintf(os.Stderr, "magi: stream idle %s (finished=%v) sid=%s\n", gap.Round(time.Second), finished, sid)
				diagLast = now // report each sustained gap once
			}
			continue
		}
		switch ev.Type {
		case port.ProviderReasoning:
			reasoning.WriteString(ev.Text)
			d, _ := json.Marshal(event.PartDeltaData{MessageID: msgID, PartID: reasonPartID, Kind: session.PartReasoning, Text: ev.Text})
			a.publishTransient(sid, event.TypePartDelta, agentActor, d)
		case port.ProviderText:
			text.WriteString(ev.Text)
			d, _ := json.Marshal(event.PartDeltaData{MessageID: msgID, PartID: textPartID, Kind: session.PartText, Text: ev.Text})
			a.publishTransient(sid, event.TypePartDelta, agentActor, d)
		case port.ProviderToolCall:
			res.toolCalls = append(res.toolCalls, ev.ToolCall)
			if ev.FromText {
				res.textConsumed = true // text was actually a tool call (fallback)
			}
		case port.ProviderUsage:
			res.usage = ev.Usage
			if ev.Usage != nil && ev.Usage.In > 0 {
				a.setPromptTokens(sid, ev.Usage.In) // real context size for meter/compaction
			}
		case port.ProviderFinish:
			finished = true
			finishAt = time.Now()
			res.finishReason = ev.FinishReason
		case port.ProviderError:
			a.emitError(ctx, sid, agentActor, ev.Err.Error())
			// A CUT stream is not a failed request. The reply arrived and then the connection
			// ended without declaring an end, which makes what was recorded a prefix — the same
			// fact finish_reason "length" states, reached by a dropped connection instead of a
			// token budget. Ending the run over it threw away everything the turn had built:
			// observed live (fix-ocaml-gc, 2026-08-01) fifteen minutes in, mid-diagnosis, magi
			// exited 1 and the trial recorded NonZeroAgentExitCode. It is recorded as an error
			// either way; only the run-ending part is dropped.
			if errors.Is(ev.Err, port.ErrStreamCut) {
				res.cut = true
				break
			}
			streamErr = true
		}
		// Reasoning-only spin guard: a single response streaming huge output with NO tool call yet
		// never finishes, so the between-steps guards can't see it. Cancel it mid-stream; the caller
		// nudges the agent to ACT instead of think forever.
		if spinCap > 0 && len(res.toolCalls) == 0 && reasoning.Len()+text.Len() > spinCap {
			res.reasoningSpun = true
			if cancel != nil {
				cancel()
			}
			break loop
		}
	}
	if streamDiag && finished {
		if d := time.Since(finishAt); d > 2*time.Second {
			fmt.Fprintf(os.Stderr, "magi: stream drained %s after finish sid=%s\n", d.Round(time.Millisecond), sid)
		}
	}
	res.text = text.String()
	res.reasoning = reasoning.String()
	if streamErr {
		return res, fmt.Errorf("provider error")
	}
	return res, nil
}
