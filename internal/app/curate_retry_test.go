package app

import (
	"context"
	"strings"
	"testing"
)

// "Reply with only JSON" is useless advice to a model whose object WAS bare and merely malformed —
// and that is the shape observed live: a 1490-char packet lost to one stretch of unquoted prose
// between two array elements. Each failure shape therefore gets the correction that fits it, and
// every branch states the stake the model cannot see (an unusable packet is not a neutral outcome:
// the worker then gets a brief that loses the verbatim identifiers).
func TestCurateRetryReminderNamesTheActualDefect(t *testing.T) {
	// The recorded shape: valid up to a point, then prose inside an array.
	malformed := `{"goal":"port it","literals":["value", the exact name matters, "count"]}`
	got := curateRetryReminder(malformed)
	if !strings.Contains(got, "the JSON itself is malformed") {
		t.Errorf("a malformed object got the wrong branch:\n%s", got)
	}
	for _, want := range []string{"between two array elements", "closed by `]`", "renames them"} {
		if !strings.Contains(got, want) {
			t.Errorf("reminder missing %q:\n%s", want, got)
		}
	}

	// Well-formed JSON that carries none of the packet's fields: a schema problem, not a syntax one,
	// so telling it about brackets would send it hunting for a defect that is not there.
	got = curateRetryReminder(`{"summary":"do the thing","steps":[]}`)
	if !strings.Contains(got, "carries none of the packet's content") {
		t.Errorf("a schema mismatch got the wrong branch:\n%s", got)
	}
	if strings.Contains(got, "closed by `]`") {
		t.Errorf("a well-formed reply was told to fix its brackets:\n%s", got)
	}
	if !strings.Contains(got, "goal, progress, missing, task") {
		t.Errorf("the schema branch must name the fields:\n%s", got)
	}

	// Pure prose with no JSON at all — the only case where "reply with only the object" is the
	// correction.
	got = curateRetryReminder("Sure! Here is what the worker should do: first, read the file.")
	if !strings.Contains(got, "ONLY the JSON object") {
		t.Errorf("a prose reply got the wrong branch:\n%s", got)
	}
	if strings.Contains(got, "malformed") {
		t.Errorf("a reply with no JSON was told its JSON was malformed:\n%s", got)
	}

	// An empty reply must still produce a usable instruction rather than an empty one.
	if r := curateRetryReminder(""); !strings.Contains(r, "ONLY the JSON object") {
		t.Errorf("an empty reply produced: %q", r)
	}
}

// A packet the recovery had to reconstruct renders a brief that reads complete, so it takes the same
// re-ask as an unreadable one. And when the re-ask cannot repair it either, the partial packet is
// still more of the task's own words than the mechanical brief — so it LANDS, named as partial,
// rather than being discarded into the fallback that loses every literal.
func TestCuratorReAsksOnADamagedPacketAndLandsThePartial(t *testing.T) {
	cut := `{"goal":"ship a KV store","task":"implement Get","literals":["GetResponse","kv.proto"`
	whole := `{"goal":"ship a KV store","task":"implement Get","literals":["GetResponse","kv.proto"],` +
		`"deliverable":"grpcurl Get returns the stored value"}`

	t.Run("repaired", func(t *testing.T) {
		llm := &auditLLM{replies: []string{cut, whole}}
		a := newOrchApp(t, llm, Config{Permission: "allow", MaxAgents: 10})
		s := parentSession(t.TempDir())
		sub := watchProgress(t, a, s.ID)
		brief, _ := a.curateDelegate(context.Background(), AgentSpec{Name: "worker"}, s,
			planStep{Title: "do it", Task: "implement Get"}, "context")
		if !strings.Contains(brief, "kv.proto") || !strings.Contains(brief, "grpcurl Get") {
			t.Errorf("the repaired packet must be used, got brief:\n%s", brief)
		}
		if n := len(llm.calls()); n != 2 {
			t.Fatalf("a damaged packet must cost exactly one re-ask (2 calls), got %d", n)
		}
		if n := sub.notes("curator"); !strings.Contains(n, "DAMAGED reply") {
			t.Errorf("the damage must be reported:\n%s", n)
		}
	})

	t.Run("still damaged", func(t *testing.T) {
		llm := &auditLLM{replies: []string{cut, cut}}
		a := newOrchApp(t, llm, Config{Permission: "allow", MaxAgents: 10})
		s := parentSession(t.TempDir())
		sub := watchProgress(t, a, s.ID)
		brief, _ := a.curateDelegate(context.Background(), AgentSpec{Name: "worker"}, s,
			planStep{Title: "do it", Task: "implement Get"}, "context")
		if !strings.Contains(brief, "GetResponse") {
			t.Errorf("a partial packet must still land — the mechanical brief keeps no literal at all:\n%s", brief)
		}
		if n := sub.notes("curator"); !strings.Contains(n, "PARTIAL") {
			t.Errorf("what landed must be named as partial:\n%s", n)
		}
	})
}

// The retry exists because the first reply is recoverable, so the second attempt has to be judged by
// the SAME bar as the first: a packet that parses but renders no context is the same loss to the
// worker as one that does not parse at all, and must not be accepted just because it came second.
func TestRenderCurateBriefRejectsAPacketWithNoContext(t *testing.T) {
	// task and tools are real fields, but neither is rendered into the brief — the task is stated
	// under the worker's own header, and tools are an allowlist, not context.
	pkt, ok := parseCuratePacket(`{"task":"do it","tools":["lsp"]}`)
	if !ok {
		t.Fatal("the packet should parse — it is well-formed and has known fields")
	}
	if b := renderCurateBrief(pkt); b != "" {
		t.Errorf("brief = %q, want empty: nothing here is context for the worker", b)
	}
}
