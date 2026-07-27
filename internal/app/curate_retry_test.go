package app

import (
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
