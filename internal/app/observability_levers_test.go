package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

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

// A stream that ERRORS midway leaves a PARTIAL reply. Returning it unmarked made a cut-off document
// indistinguishable from a badly-formed one, so every caller reported "unparseable" and pointed the
// diagnosis at the model's JSON when the real event was a broken stream. The partial text must
// still be returned — salvage wants it — but the cut must come back with it.
func TestDrainStreamReturnsThePartialAndTheCut(t *testing.T) {
	llm := &cutOffLLM{text: `{"criteria":["the build passes","the tests `}
	stream, err := llm.StreamChat(context.Background(), port.ChatRequest{})
	if err != nil {
		t.Fatal(err)
	}
	text, cut := drainStream(stream)
	if !strings.Contains(text, "the build passes") {
		t.Fatalf("the partial text must still be returned for salvage, got %q", text)
	}
	if cut == nil || !strings.Contains(cut.Error(), "boom") {
		t.Errorf("the cut must be reported with its cause, got %v", cut)
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
