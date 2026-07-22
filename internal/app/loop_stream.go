package app

import (
	"context"
	"encoding/json"
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
}

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

// drainText consumes a TEXT-ONLY provider stream (the planner and other tool-free side calls that only
// accumulate the reply, unlike consumeStream which also publishes deltas). It carries NO hang watchdog
// of its own: the provider is wrapped by guardedProvider (GuardProvider), which aborts a silent or
// spinning generation at the receive point, so a hung re-plan generate closes the stream and drainText
// returns (usually empty) — which flows into the caller's own no-result path (the planner's JSON-only
// retry). Returns the accumulated text and any StreamChat transport error.
func (a *App) drainText(ctx context.Context, spec AgentSpec, req port.ChatRequest) (string, error) {
	stream, err := a.providerFor(spec).StreamChat(ctx, req)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for ev := range stream {
		if ev.Type == port.ProviderText {
			b.WriteString(ev.Text)
		}
	}
	return b.String(), nil
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
	spinCap := reasoningSpinCap()
	if a.cfg.MaxOutputTokens > 0 {
		spinCap = 0 // [limits] max_output_tokens caps each response at the token level — defer to it
	}
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
		case port.ProviderError:
			a.emitError(ctx, sid, agentActor, ev.Err.Error())
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
