package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/port"
)

// An EMPTY execution note must say which pass came up empty. Mining is best-effort, but the note is
// the run's only record of the literals a grader checks verbatim, so silence made "nothing to mine"
// and "the distill never parsed" identical in the log — on the one task where record formats
// mattered most, the note was simply absent with no trace of why.
func TestSpecMineReportsWhyTheNoteIsEmpty(t *testing.T) {
	cases := []struct {
		name, reply, want string
	}{
		{"analysis empty", "", "analysis pass returned nothing"},
		{"nothing to mine", "NONE", "found nothing to mine"},
		{"distill unparseable", "a real analysis with content", "distill pass did not parse"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := newOrchApp(t, &gateLLM{text: c.reply}, Config{Permission: "allow", MaxAgents: 10})
			s := parentSession(t.TempDir())
			sub := watchProgress(t, a, s.ID)
			if note := a.elicitSpecMine(context.Background(), AgentSpec{Name: "planner"}, s, "do the thing"); note != "" {
				t.Fatalf("this reply must not produce a note, got %q", note)
			}
			if got := sub.notes("spec-mine"); !strings.Contains(got, "no execution note") || !strings.Contains(got, c.want) {
				t.Errorf("want a reason naming %q, got:\n%s", c.want, got)
			}
		})
	}
}

// A tool name that differs from a REGISTERED one only in separators/case must be named back, and
// the reply must never deny a tool the same reply lists. It used to append "there is no todo/plan
// tool" unconditionally while todowrite was registered and listed — the model was told, in one
// message, both that the tool exists and that it does not.
func TestNearestToolName(t *testing.T) {
	names := []string{"bash", "todowrite", "bash_output", "report"}
	cases := []struct{ called, want string }{
		{"todo_write", "todowrite"},   // the observed miss
		{"TodoWrite", "todowrite"},    // case only
		{"todo-write", "todowrite"},   // another separator
		{"bashOutput", "bash_output"}, // camel vs snake
		{"bash", ""},                  // exact hit is not a suggestion
		{"run", ""},                   // NOT fuzzy: never guess a tool the model didn't ask for
		{"finish", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := nearestToolName(c.called, names); got != c.want {
			t.Errorf("nearestToolName(%q) = %q, want %q", c.called, got, c.want)
		}
	}
}

// A trailing `&& echo "…"` labels a result; it changes nothing. Two commands differing only there
// do identical work, yet the full-command fingerprint made each one FIRST-SEEN — and a first-seen
// exercising command is credited as forward motion, so relabelled repeats of one verification kept
// the stall watchdog from ever engaging. The label is stripped for fingerprinting only.
func TestStripEchoTail(t *testing.T) {
	cases := []struct{ in, want string }{
		// the observed shape: same work, different label
		{`node x.js > out && diff -q a out && echo "PASS"`, `node x.js > out && diff -q a out`},
		{`node x.js > out && diff -q a out && echo "VERIFIED: Complete"`, `node x.js > out && diff -q a out`},
		{`make && echo done`, `make`},
		{`make ; echo done`, `make`},
		{`make || echo failed`, `make`},
		{`make && echo -n 'ok'`, `make`},
		{`a && echo one && echo two`, `a`}, // chained labels collapse
		// left alone: no tail, a substitution that can differ per run, and a bare echo
		{`make`, `make`},
		{"a && echo \"$RESULT\"", "a && echo \"$RESULT\""},
		{"a && echo $(date)", "a && echo $(date)"},
		{`echo hello`, `echo hello`}, // the command IS the echo — keep it
	}
	for _, c := range cases {
		if got := stripEchoTail(c.in); got != c.want {
			t.Errorf("stripEchoTail(%q)\n  = %q\n want %q", c.in, got, c.want)
		}
	}
}

// guardArgs must therefore give two relabelled runs of the same verification ONE fingerprint, while
// keeping genuinely different work apart.
func TestGuardArgsCollapsesRelabelledBash(t *testing.T) {
	fp := func(cmd string) string {
		b, _ := json.Marshal(map[string]string{"command": cmd})
		return guardArgs("bash", b)
	}
	a := fp(`node x.js > out && diff -q ref out && echo "PASS"`)
	b := fp(`node x.js > out && diff -q ref out && echo "VERIFIED"`)
	if a != b {
		t.Errorf("relabelled repeats must share a fingerprint:\n a=%s\n b=%s", a, b)
	}
	if c := fp(`node y.js > out && diff -q ref out && echo "PASS"`); c == a {
		t.Error("a different command must NOT collapse onto the same fingerprint")
	}
	// A tail-less command is untouched (identical to the plain canonical form).
	plain, _ := json.Marshal(map[string]string{"command": "make test"})
	if fp("make test") != canonicalArgs(plain) {
		t.Error("a command with no echo tail must fingerprint exactly as before")
	}
}

// Every reader of model-produced JSON must survive the defects those payloads normally carry —
// a raw newline inside a multi-line string, a trailing comma — because rejecting the document over
// one discards content that was otherwise complete. Observed: a coverage fill's whole reply thrown
// away, leaving a five-step plan with no executable check at all.
func TestLenientReadersAcrossPayloads(t *testing.T) {
	// curator packet: task/goal are multi-line prose.
	pkt, ok := parseCuratePacket("{\"task\":\"do X\nthen Y\",\"literals\":[\"value\"],}")
	if !ok || !strings.Contains(string(pkt.Task), "then Y") {
		t.Fatalf("curate packet with a newline + trailing comma must parse: ok=%v %+v", ok, pkt)
	}
	// A genuinely malformed document still fails — leniency is not "accept anything".
	if _, ok := parseCuratePacket(`{"task" "x"}`); ok {
		t.Error("an irreparable packet must not parse")
	}
}

// Every reader of a model reply must SAY when it recovered nothing. Each of these silently
// degraded the run into a different, worse mode while the log looked identical to the good path:
// the curator fell back to the mechanical brief that loses the verbatim identifiers, and the
// contract gate proceeded with an empty draft as if none had been asked for.
func TestModelReplyFailuresAreReported(t *testing.T) {
	prose := "I cannot produce that structure for this task."

	t.Run("curator packet", func(t *testing.T) {
		a := newOrchApp(t, &gateLLM{text: prose}, Config{Permission: "allow", MaxAgents: 10})
		s := parentSession(t.TempDir())
		// A real session, not just the struct: the diagnostic record asserted below is written to the
		// store, and a session that was never created can only ever report having kept nothing.
		sid, err := a.CreateSession(context.Background(), command.CreateSession{Workdir: s.Workdir})
		if err != nil {
			t.Fatal(err)
		}
		s.ID = sid
		sub := watchProgress(t, a, s.ID)
		brief, tools := a.curateDelegate(context.Background(), AgentSpec{Name: "worker"}, s,
			planStep{Title: "do it", Task: "do the thing"}, "context")
		if brief != "" || tools != nil {
			t.Fatalf("an unusable packet must fall back, got brief=%q tools=%v", brief, tools)
		}
		if n := sub.notes("curator"); !strings.Contains(n, "mechanical brief") || !strings.Contains(n, "cannot produce") {
			t.Errorf("the fallback must be reported with the reply:\n%s", n)
		}
		// The progress line above is bus-only and whitespace-collapsed, so it is not a record. Until the
		// re-ask was shared, only the planner persisted the reply it could not read — this pass, which
		// falls back to a brief that loses the verbatim identifiers, kept nothing at all.
		if ds := diagnostics(t, a, s.ID); len(ds) != 2 {
			t.Errorf("both unusable curator replies must be persisted as diagnostics, got %+v", ds)
		}
	})

	t.Run("contract draft", func(t *testing.T) {
		a := newOrchApp(t, &gateLLM{text: prose}, Config{Permission: "allow", MaxAgents: 10})
		s := parentSession(t.TempDir())
		sub := watchProgress(t, a, s.ID)
		if got := a.elicitContractDraft(context.Background(), AgentSpec{Name: "planner"}, s.ID, "m", "task"); got != nil {
			t.Fatalf("prose must yield no criteria, got %v", got)
		}
		if n := sub.notes("contract-draft"); !strings.Contains(n, "author from scratch") || !strings.Contains(n, "cannot produce") {
			t.Errorf("an empty draft must be reported with the reply:\n%s", n)
		}
	})
}

// A stream that ERRORS midway leaves a PARTIAL reply. Returning it unmarked made a cut-off document
// indistinguishable from a badly-formed one, so every caller reported "unparseable" and pointed the
// diagnosis at the model's JSON when the real event was a broken stream. The partial text must
// still be returned — salvage wants it — but the cut must be reported.
func TestSideCallReportsATruncatedStream(t *testing.T) {
	a := newOrchApp(t, &cutOffLLM{text: `{"criteria":["the build passes","the tests `},
		Config{Permission: "allow", MaxAgents: 10})
	s := parentSession(t.TempDir())
	sub := watchProgress(t, a, s.ID)
	got := a.specMineCall(context.Background(), AgentSpec{Name: "planner"}, s.ID, "contract-draft", "m", "sys", "user")
	if !strings.Contains(got, "the build passes") {
		t.Fatalf("the partial text must still be returned for salvage, got %q", got)
	}
	note := sub.notes("contract-draft")
	if !strings.Contains(note, "CUT OFF") || !strings.Contains(note, "boom") {
		t.Errorf("the cut must be reported with its cause:\n%s", note)
	}
}

// cutOffLLM streams some text and then an error, the shape of a deadline firing mid-generation.
type cutOffLLM struct{ text string }

func (c *cutOffLLM) StreamChat(context.Context, port.ChatRequest) (<-chan port.ProviderEvent, error) {
	ch := make(chan port.ProviderEvent, 3)
	ch <- port.ProviderEvent{Type: port.ProviderText, Text: c.text}
	ch <- port.ProviderEvent{Type: port.ProviderError, Err: errors.New("boom")}
	close(ch)
	return ch, nil
}
