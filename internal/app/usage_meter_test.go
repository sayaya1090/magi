package app

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// usageLLM reports usage on every call, like a real backend asked for include_usage.
type meterLLM struct{ in, out int }

func (f *meterLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	ch := make(chan port.ProviderEvent, 3)
	ch <- port.ProviderEvent{Type: port.ProviderText, Text: "ok"}
	ch <- port.ProviderEvent{Type: port.ProviderUsage, Usage: &event.Usage{In: f.in, Out: f.out}}
	ch <- port.ProviderEvent{Type: port.ProviderFinish}
	close(ch)
	return ch, nil
}

func drain(t *testing.T, a *App, ctx context.Context, p port.LLMProvider) {
	t.Helper()
	ch, err := p.StreamChat(ctx, port.ChatRequest{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for range ch {
		n++
	}
	if n != 3 {
		t.Fatalf("the meter must pass the stream through unchanged, got %d events", n)
	}
}

// The billing number must count EVERY request — the agent's stream, the council's polls, every side
// call — not the one place the turn meter happened to read. And a request on an untagged context
// still has to land in it, or a plumbing gap silently shrinks the bill.
func TestUsageTotalCountsEveryRequest(t *testing.T) {
	a := newShellApp(t, &shellPlatform{})
	p := a.MeterProvider(&meterLLM{in: 100, out: 10})
	ctx := context.Background()

	drain(t, a, ctxWithUsageSID(ctx, "s_main"), p)
	drain(t, a, ctxWithUsageSID(ctx, "s_main"), p) // a second step: prompts SUM, they do not replace
	drain(t, a, ctx, p)                            // untagged (a call site nobody plumbed)

	tot := a.UsageTotal()
	if tot.In != 300 || tot.Out != 30 {
		t.Errorf("UsageTotal = in %d / out %d; want 300/30", tot.In, tot.Out)
	}
	if got := a.UsageFor("s_main"); got.In != 200 || got.Out != 20 {
		t.Errorf("attributed usage = in %d / out %d; want 200/20", got.In, got.Out)
	}
	// Wrapping twice must not double-count.
	if a.MeterProvider(p) != p {
		t.Error("MeterProvider must be idempotent")
	}
}

// A delegated step's tokens belong to the run that delegated it. Counting only the parent's own
// session reports near-zero for exactly the turns that cost the most.
func TestUsageRollsUpFromChildren(t *testing.T) {
	a := newShellApp(t, &shellPlatform{})
	wd := t.TempDir()
	spawn := func(id, parent session.SessionID) {
		a.mu.Lock()
		a.stateLocked(id).meta = session.Session{ID: id, Workdir: wd, Parent: parent}
		a.mu.Unlock()
	}
	spawn("s_main", "")
	spawn("s_kid", "s_main")
	spawn("s_grand", "s_kid")

	a.recordUsage("s_main", "m", event.Usage{In: 10, Out: 1})
	a.recordUsage("s_kid", "m", event.Usage{In: 100, Out: 20})
	a.recordUsage("s_grand", "m", event.Usage{In: 1000, Out: 300})

	got := a.UsageFor("s_main")
	if got.In != 1110 || got.Out != 321 {
		t.Errorf("UsageFor(main) = in %d / out %d; want 1110/321 (self + child + grandchild)", got.In, got.Out)
	}
	if own := a.UsageFor("s_grand"); own.In != 1000 {
		t.Errorf("a leaf reports only itself, got %d", own.In)
	}
	if tot := a.UsageTotal(); tot.In != 1110 {
		t.Errorf("the grand total must match, got %d", tot.In)
	}
}

// A finished turn must report the BILL — every request under it, subagents included — not the
// agent's own stream with In holding only the last prompt.
func TestTurnUsageReportsTheBill(t *testing.T) {
	a := newShellApp(t, &shellPlatform{})
	wd := t.TempDir()
	spawn := func(id, parent session.SessionID) {
		a.mu.Lock()
		a.stateLocked(id).meta = session.Session{ID: id, Workdir: wd, Parent: parent}
		a.mu.Unlock()
	}
	spawn("s_root", "")
	spawn("s_kid", "s_root")

	start := a.UsageFor("s_root")
	// Two steps of the agent's own stream, a council poll, and a subagent's work.
	a.recordUsage("s_root", "m", event.Usage{In: 500, Out: 50})
	a.recordUsage("s_root", "m", event.Usage{In: 900, Out: 40})
	a.recordUsage("s_root", "m", event.Usage{In: 300, Out: 20}) // council/side call, same session
	a.recordUsage("s_kid", "m", event.Usage{In: 2000, Out: 700})

	// The old accounting would have said In=900 (last prompt) / Out=90 (own stream only).
	u := turnUsage(a, "s_root", start, 900, 90, 0)
	if u.In != 3700 || u.Out != 810 {
		t.Errorf("turn usage = in %d / out %d; want 3700/810 (every request, subagent included)", u.In, u.Out)
	}

	// A backend that reports no usage at all must not turn into a zero: the stream totals stand in.
	b := newShellApp(t, &shellPlatform{})
	if f := turnUsage(b, "s_none", event.Usage{}, 900, 90, 1.25); f.In != 900 || f.Out != 90 || f.Cost != 1.25 {
		t.Errorf("with nothing metered the old accounting stands in, got %+v", f)
	}
	// Only THIS turn's delta counts — what was spent before it started is not billed again.
	mid := a.UsageFor("s_root")
	a.recordUsage("s_root", "m", event.Usage{In: 7, Out: 3})
	if u2 := turnUsage(a, "s_root", mid, 0, 0, 0); u2.In != 7 || u2.Out != 3 {
		t.Errorf("second turn = in %d / out %d; want 7/3", u2.In, u2.Out)
	}
}

// A turn can finish two ways — the model stops calling tools, or it files a report — and each built
// its own usage. The report path kept the agent's-stream-only accounting, so a turn that ended that
// way published the LAST prompt where the bill was. Measured on a headless run:
//
//	magi: tokens in 1721335 / out 4880
//	turn.finished {"usage":{"in":48519,"out":4880}}
//
// The outputs agree and the inputs are thirty-five fold apart, which is that construction exactly.
func TestBothFinishPathsUseTheSameAccounting(t *testing.T) {
	src, err := os.ReadFile("loop.go")
	if err != nil {
		t.Fatal(err)
	}
	// The old shape, spelled out. It has exactly one legitimate home: turnUsage's fallback, for a
	// backend that reports no usage at all. Anywhere BEFORE that function is a finish path keeping
	// its own books.
	const oldShape = "event.Usage{In: lastIn, Out: cumOut, Cost: cumCost}"
	decl := bytes.Index(src, []byte("func turnUsage("))
	if decl < 0 {
		t.Fatal("turnUsage is gone — the accounting moved and this guard needs to move with it")
	}
	if n := bytes.Count(src, []byte(oldShape)); n != 1 {
		t.Errorf("the stream-locals accounting must exist once (turnUsage's fallback), found %d", n)
	}
	if bytes.Contains(src[:decl], []byte(oldShape)) {
		t.Error("a finish path is building its own usage from the stream locals — turnUsage is the one accounting")
	}
	if n := bytes.Count(src, []byte("turnUsage(a, sid, usageAtStart")); n != 2 {
		t.Errorf("both finish paths must go through turnUsage, found %d call(s)", n)
	}
}
