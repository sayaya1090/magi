package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// toolCallEv builds one tool-call event with the given name and raw JSON args.
func toolCallEv(id, name, args string) event.Event {
	d := event.PartAppendedData{Part: session.Part{
		Kind:     session.PartToolCall,
		ToolCall: &session.ToolCall{CallID: id, Name: name, Args: json.RawMessage(args)},
	}}
	b, _ := json.Marshal(d)
	return event.Event{Type: event.TypePartAppended, Data: b}
}

// The record used to hold every read magi granted and hand back none of them. Observed live: the
// same region of one file opened thirteen times through four mechanisms, each call carrying a
// different window so no two shared a fingerprint — and every one of them inspect-only, so the
// record filed it as neither ran-clean nor failed.
func TestRecordSaysWhatWasReadTwice(t *testing.T) {
	const p = "/app/ocaml/runtime/shared_heap.c"
	evs := []event.Event{
		toolCallEv("1", "read", `{"path":"`+p+`","offset":540}`),
		toolCallEv("2", "read", `{"path":"`+p+`","offset":640}`),
		// Named from inside its own directory, by a command that only prints it.
		toolCallEv("3", "bash", `{"command":"cd /app/ocaml/runtime && sed -n '570,652p' shared_heap.c"}`),
		toolCallEv("4", "read", `{"path":"/app/ocaml/HACKING.adoc"}`),
	}
	o := observeEvents(evs)
	if got := o.looked[p]; got != 3 {
		t.Fatalf("three looks at the same file must count three, got %d", got)
	}
	again := o.reread()
	if len(again) != 1 || !strings.Contains(again[0], p) || !strings.HasSuffix(again[0], "×3") {
		t.Fatalf("only the repeated path is worth stating, got %v", again)
	}
	if txt := o.render(); !strings.Contains(txt, "already looked at more than once") ||
		!strings.Contains(txt, "×3") || strings.Contains(txt, "HACKING") {
		t.Errorf("the record must state the repeat and nothing read once:\n%s", txt)
	}
}

// A shell token that merely looks like a filename is not evidence the agent read it: it may be a
// glob, a redirect artifact, or an argument. Only a path the read tool already established gets
// counted again, so the record can be incomplete but never wrong about what it observed.
func TestRereadNeverInventsAPath(t *testing.T) {
	o := observeEvents([]event.Event{
		toolCallEv("1", "bash", `{"command":"cat notes.txt && grep -n foo *.c"}`),
	})
	if len(o.reread()) != 0 || len(o.looked) != 0 {
		t.Fatalf("a bare command must not put paths into the record, got %v", o.looked)
	}
	// Reading it first is what makes the later mention countable.
	o = observeEvents([]event.Event{
		toolCallEv("1", "read", `{"path":"notes.txt"}`),
		toolCallEv("2", "bash", `{"command":"cat notes.txt"}`),
	})
	if got := o.looked["notes.txt"]; got != 2 {
		t.Errorf("a read then a cat of the same file is two looks, got %d", got)
	}
}

// A command that RUNS something is not looking, even when it names a file it happens to compile.
func TestExercisingCommandIsNotALook(t *testing.T) {
	o := observeEvents([]event.Event{
		toolCallEv("1", "read", `{"path":"main.c"}`),
		toolCallEv("2", "bash", `{"command":"gcc -o main main.c && ./main"}`),
	})
	if got := o.looked["main.c"]; got != 1 {
		t.Errorf("building a file is not re-reading it, got %d looks", got)
	}
}

// The busiest path is named first: a reader that stops at the first entry has the one that matters.
func TestRereadOrdersByHowOftenItWasOpened(t *testing.T) {
	evs := []event.Event{
		toolCallEv("1", "read", `{"path":"a.c"}`),
		toolCallEv("2", "read", `{"path":"a.c"}`),
		toolCallEv("3", "read", `{"path":"b.c"}`),
		toolCallEv("4", "read", `{"path":"b.c"}`),
		toolCallEv("5", "read", `{"path":"b.c"}`),
	}
	got := observeEvents(evs).reread()
	if len(got) != 2 || !strings.HasPrefix(got[0], "b.c") {
		t.Errorf("most-opened first, got %v", got)
	}
}

// `cd runtime && sed -n '1,80p' heap.c` is one look at heap.c and none at the directory: stepping
// into a directory is how the agent got somewhere, not a read of what is in it.
func TestChangingDirectoryIsNotALook(t *testing.T) {
	o := observeEvents([]event.Event{
		toolCallEv("1", "list", `{"path":"/app/runtime"}`),
		toolCallEv("2", "read", `{"path":"/app/runtime/heap.c"}`),
		toolCallEv("3", "bash", `{"command":"cd /app/runtime && sed -n '1,80p' heap.c"}`),
	})
	if got := o.looked["/app/runtime"]; got != 1 {
		t.Errorf("cd must not count as looking at the directory, got %d", got)
	}
	if got := o.looked["/app/runtime/heap.c"]; got != 2 {
		t.Errorf("the sed is the second look at the file, got %d", got)
	}
}

// A run that never touches the read tool got nothing from the path counter, because that counter
// only credits a shell command against a path a read already opened. Observed live on
// sqlite-with-gcov: 52 bash calls, ZERO reads, and one `ls … && nm … | grep __gcov | wc -l` issued
// five times byte-for-byte while the record said nothing at all.
func TestRepeatedInspectCommandsAreCounted(t *testing.T) {
	// The live command, JSON-escaped exactly as the model sends it.
	const probe = `{"command":"ls -la /app/sqlite/sqlite3 && nm /app/sqlite/libsqlite3.so | grep \"__gcov\" | wc -l"}`
	evs := []event.Event{
		toolCallEv("1", "bash", probe),
		// Same command, different whitespace — one repeat, not two commands.
		toolCallEv("2", "bash", `{"command":"  ls -la /app/sqlite/sqlite3   &&  nm /app/sqlite/libsqlite3.so | grep \"__gcov\" | wc -l "}`),
		toolCallEv("3", "bash", probe),
		toolCallEv("4", "bash", `{"command":"ls /tmp"}`),
	}
	o := observeEvents(evs)
	again := o.repeatedCommands()
	if len(again) != 1 {
		t.Fatalf("only the repeated command is worth stating, got %v", again)
	}
	if !strings.Contains(again[0], "libsqlite3.so") || !strings.HasSuffix(again[0], "×3") {
		t.Errorf("whitespace must not split one command into three, got %q", again[0])
	}
	if txt := o.render(); !strings.Contains(txt, "issued more than once, exactly as written") {
		t.Errorf("the repeat must reach the record:\n%s", txt)
	}
}

// A repeated build is reported as a repeat but never as a LOOK: the two lines say different things
// and neither may claim the other's meaning.
func TestRepeatedBuildIsARepeatNotALook(t *testing.T) {
	o := observeEvents([]event.Event{
		toolCallEv("1", "bash", `{"command":"make -j4 world"}`),
		toolCallEv("2", "bash", `{"command":"make -j4 world"}`),
		toolCallEv("3", "bash", `{"command":"make -j4 world"}`),
	})
	if got := o.reread(); len(got) != 0 {
		t.Errorf("re-running a build is not re-reading it, got %v", got)
	}
	if got := o.repeatedCommands(); len(got) != 1 || !strings.HasSuffix(got[0], "×3") {
		t.Errorf("but it IS a repeat and must be stated, got %v", got)
	}
}
