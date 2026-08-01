package llm

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// The task from the false done this exists for. It names `value` for the REQUEST and `val` for
// both responses, two lines apart — and the agent wrote `val` everywhere, normalising to the
// identifier it had just typed twice. Its own end-to-end test passed because it called its own
// stubs, and three members voted done at full confidence with the task and the proto both in
// front of them.
const kvStoreTask = `2. Create a file /app/kv-store.proto containing a service called KVStore, which creates two RPCs:
  a. GetVal takes a message named GetValRequest that includes a key (string) as a parameter and returns a GetValResponse with a val (int) field
  b. SetVal takes a message named SetValRequest that includes a key (string) and a value (int) as parameters and returns a SetValResponse with a val (int) field
3. Generate the Python code (protobuf generates two python files: {class name}_pb2.py and {class name}_pb2_grpc.py) and place them in /app.
4. Create /app/server.py, implement the KVStore service in a class called Server. You will use port 5328.`

const kvProtoWrong = `+++ b/app/kv-store.proto
message SetValRequest { string key = 1; int32 val = 2; }
message SetValResponse { int32 val = 1; }
message GetValRequest { string key = 1; }
message GetValResponse { int32 val = 1; }
service KVStore { rpc GetVal(GetValRequest) returns (GetValResponse); rpc SetVal(SetValRequest) returns (SetValResponse); }
+++ b/app/server.py
class Server: pass`

var kvProtoRight = strings.Replace(kvProtoWrong, "int32 val = 2;", "int32 value = 2;", 1)

func TestTheOneIdentifierTheWorkNeverUsedIsNamed(t *testing.T) {
	missing := missingLiterals(kvStoreTask, kvProtoWrong)
	if len(missing) != 1 || missing[0] != "value" {
		t.Fatalf("want exactly [value], got %v", missing)
	}
	sec := literalsSection(kvStoreTask, kvProtoWrong)
	if !strings.Contains(sec, "value") {
		t.Errorf("the section does not name it: %q", sec)
	}
	// It states the comparison and stops. Anything that reaches for the verdict — telling the
	// member how to vote, or calling the absence a defect on magi's behalf — would be magi voting.
	for _, forbidden := range []string{"you must", "vote continue", "vote done", "this is a defect", "is missing and must"} {
		if strings.Contains(strings.ToLower(sec), forbidden) {
			t.Errorf("the section instructs rather than measures (%q):\n%s", forbidden, sec)
		}
	}
	// …and it hands the judgement back explicitly.
	if !strings.Contains(sec, "yours to judge") {
		t.Errorf("the section does not leave the call to the member:\n%s", sec)
	}
}

// The correct implementation is SILENT. This is the half that decides whether the measurement is
// usable at all: a section that fires on a right answer is churn, and this council has been burned
// by exactly that before.
func TestACorrectImplementationDrawsNothing(t *testing.T) {
	if missing := missingLiterals(kvStoreTask, kvProtoRight); len(missing) != 0 {
		t.Errorf("a correct implementation was reported as missing %v", missing)
	}
	if sec := literalsSection(kvStoreTask, kvProtoRight); sec != "" {
		t.Errorf("a correct implementation drew a section:\n%s", sec)
	}
}

// A placeholder is not a name. "{class name}_pb2.py" made the first version fire on a correct run,
// which is the one thing this must never do.
func TestAPlaceholderFragmentIsNotAnIdentifier(t *testing.T) {
	for _, lit := range literalsInTask(kvStoreTask) {
		if strings.HasPrefix(lit, "_") {
			t.Errorf("a fragment was taken as an identifier: %q", lit)
		}
	}
}

// Whole words only. "value" inside "values" is not the field the task asked for, and counting it
// as present is how this measurement would quietly stop working.
func TestAWordInsideAnotherWordIsNotTheIdentifier(t *testing.T) {
	if containsWord("records number values for keys", "value") {
		t.Error("\"values\" was read as \"value\"")
	}
	if !containsWord("int32 value = 2;", "value") {
		t.Error("the real field was not found")
	}
	if !containsWord("value", "value") {
		t.Error("a whole string that IS the identifier was not found")
	}
}

// A turn that wrote nothing says nothing about identifiers — an investigation or an answer has no
// files to contain them, and reporting all of them missing would be the reflexive demand this
// council avoids.
func TestATurnThatWroteNothingIsNotAccused(t *testing.T) {
	if missing := missingLiterals(kvStoreTask, "   "); len(missing) != 0 {
		t.Errorf("a turn with no work was reported as missing %v", missing)
	}
}

// End to end: the section reaches the members, and the flag removes it.
func TestTheSectionReachesTheMembers(t *testing.T) {
	req := port.DeliberationRequest{Round: 1, Task: kvStoreTask, Changes: kvProtoWrong}
	// Members are polled in parallel, so the capture needs a lock — the reply itself is the same
	// for all three and only the text matters.
	var mu sync.Mutex
	var seen string
	c := New(only(fakeLLM{reply: func(r port.ChatRequest) string {
		mu.Lock()
		seen = textOf(r)
		mu.Unlock()
		return `{"decision":"done","rationale":"looks fine","cite":"NO-EVIDENCE"}`
	}}), "m")
	if _, err := c.Deliberate(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	got := seen
	mu.Unlock()
	if !strings.Contains(got, "Identifiers the task names") || !strings.Contains(got, "value") {
		t.Errorf("the measurement did not reach the member:\n%s", got)
	}

	t.Setenv("MAGI_COUNCIL_LITERALS", "0")
	mu.Lock()
	seen = ""
	mu.Unlock()
	if _, err := c.Deliberate(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Contains(seen, "Identifiers the task names") {
		t.Error("the flag did not remove the section")
	}
}

// An example is not a specification. Swept across eleven real task prompts, the shapes that can
// never match are a placeholder in a path, a quoted escape, a signature fragment, and a token the
// task only ever writes inside quotes — `call setreg('a', "your_keystrokes_here")` expects the
// solution to REPLACE that, so reporting its absence accuses a correct run.
func TestExamplesAndPlaceholdersAreNotIdentifiers(t *testing.T) {
	for _, c := range []struct{ name, task, gone string }{
		{"path placeholder", "unpack it into `/app/pmars-<version>/` and build", "pmars-<version>"},
		{"brace placeholder", "protobuf generates {class name}_pb2.py in /app", "_pb2.py"},
		{"quoted escape", "send `\"\\x03\"` to interrupt", "x03"},
		{"signature fragment", "define `HeadlessTerminal(BaseTerminal)` in it", "HeadlessTerminal(BaseTerminal)"},
		{"quoted example", `write call setreg('a', "your_keystrokes_here") in apply_macros.vim`, "your_keystrokes_here"},
	} {
		for _, lit := range literalsInTask(c.task) {
			if strings.Contains(lit, c.gone) {
				t.Errorf("%s: %q was taken as an identifier the work must contain", c.name, lit)
			}
		}
	}
	// …while the identifier named in prose beside it still comes through. The extension list is
	// finite by design — .vim is not on it — so what survives here is the snake_case name, which
	// is the part the work has to contain either way.
	got := literalsInTask(`write call setreg('a', "your_keystrokes_here") in apply_macros.vim`)
	if len(got) != 1 || got[0] != "apply_macros" {
		t.Errorf("the real identifier did not survive: %v", got)
	}
}

// A task names a file by its absolute path; the evidence names it relative to the workdir. Compared
// literally that candidate can never match, so a file the agent had written was reported to the
// members as absent — the one thing this measurement must not do.
//
// Measured over 138 recorded council rounds: 46 fired and 32 of the reported items were this.
// After: 15 fired, none of them a path the work carried.
func TestAPathTheWorkCarriesUnderItsRelativeNameIsNotMissing(t *testing.T) {
	// The shape the evidence really has: buildCouncilChanges heads each file with its
	// workdir-relative path.
	work := "### run.py (current content, full)\nimport asyncio\n\nasync def run_tasks(): ...\n"
	for _, tc := range []struct {
		what, task string
		want       []string
	}{
		// An absolute path becomes a candidate through the backticks a task writes it in —
		// dottedFile alone cannot span a `/`, so it yields the bare name.
		{"backticked absolute path, relative in work", "Implement `/app/run.py` with run_tasks", nil},
		{"backticked path genuinely absent", "Implement `/app/server.py`", []string{"/app/server.py", "server.py"}},
		{"bare absolute path yields the file name", "Implement /app/run.py", nil},
		{"a bare name stays exact", "the field is a value (int)", []string{"value"}},
	} {
		t.Run(tc.what, func(t *testing.T) {
			got := missingLiterals(tc.task, work)
			if len(got) != len(tc.want) {
				t.Fatalf("missing = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("missing = %v, want %v", got, tc.want)
				}
			}
		})
	}
	// The relaxation is only the last segment of a path — it must not turn into substring matching.
	if !presentInWork("### run.py (x)", "/app/run.py") {
		t.Error("a path whose base the work carries should count as present")
	}
	if presentInWork("### values.py (x)", "value") {
		t.Error("a bare identifier must not be satisfied by a longer word")
	}
}
