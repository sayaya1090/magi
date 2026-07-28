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

// The record counts what magi SERVED: a path the read tool opened. It used to also credit a bash
// command as a look when a verb table called that command inspect-only — so `cat`, `sed -n` and
// `grep` were looks and `make` was not, decided by a hand-maintained list. The repeats those
// commands make are already a fact under their own line ("issued more than once, exactly as
// written"), so the guess bought nothing and could be wrong.
func TestRecordSaysWhatWasReadTwice(t *testing.T) {
	const p = "/app/ocaml/runtime/shared_heap.c"
	evs := []event.Event{
		toolCallEv("1", "read", `{"path":"`+p+`","offset":540}`),
		toolCallEv("2", "read", `{"path":"`+p+`","offset":640}`),
		// A shell command naming the same file is NOT counted: magi did not serve that read.
		toolCallEv("3", "bash", `{"command":"cd /app/ocaml/runtime && sed -n '570,652p' shared_heap.c"}`),
		toolCallEv("4", "read", `{"path":"/app/ocaml/HACKING.adoc"}`),
	}
	o := observeEvents(evs)
	if got := o.looked[p]; got != 2 {
		t.Fatalf("two reads magi served count two, got %d", got)
	}
	again := o.reread()
	if len(again) != 1 || !strings.Contains(again[0], p) || !strings.HasSuffix(again[0], "×2") {
		t.Fatalf("only the repeated path is worth stating, got %v", again)
	}
	if txt := o.render(); !strings.Contains(txt, "already looked at more than once") ||
		!strings.Contains(txt, "×2") || strings.Contains(txt, "HACKING") {
		t.Errorf("the record must state the repeat and nothing read once:\n%s", txt)
	}
}

// A shell token that merely looks like a filename is not evidence of anything: it may be a glob, a
// redirect artifact, or an argument. The record counts reads magi performed, so it can be
// incomplete but never wrong about what it observed.
func TestRereadNeverInventsAPath(t *testing.T) {
	o := observeEvents([]event.Event{
		toolCallEv("1", "bash", `{"command":"cat notes.txt && grep -n foo *.c"}`),
	})
	if len(o.reread()) != 0 || len(o.looked) != 0 {
		t.Fatalf("a bare command must not put paths into the record, got %v", o.looked)
	}
	// Nor does mentioning it after a real read: the second look never happened through magi.
	o = observeEvents([]event.Event{
		toolCallEv("1", "read", `{"path":"notes.txt"}`),
		toolCallEv("2", "bash", `{"command":"cat notes.txt"}`),
	})
	if got := o.looked["notes.txt"]; got != 1 {
		t.Errorf("one read is one look; the cat is its own line, got %d", got)
	}
	// …and that cat still reaches the record as the command it was.
	if txt := o.render(); !strings.Contains(txt, "cat notes.txt") {
		t.Errorf("the command itself is a fact and must be listed:\n%s", txt)
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

// Tool calls are looks; shell commands are commands. `list` opened the directory and `read` opened
// the file — one each — and the `cd … && sed -n` that follows adds to neither, because magi served
// no read for it.
func TestOnlyServedReadsAreLooks(t *testing.T) {
	o := observeEvents([]event.Event{
		toolCallEv("1", "list", `{"path":"/app/runtime"}`),
		toolCallEv("2", "read", `{"path":"/app/runtime/heap.c"}`),
		toolCallEv("3", "bash", `{"command":"cd /app/runtime && sed -n '1,80p' heap.c"}`),
	})
	if got := o.looked["/app/runtime"]; got != 1 {
		t.Errorf("the directory was listed once, got %d", got)
	}
	if got := o.looked["/app/runtime/heap.c"]; got != 1 {
		t.Errorf("the file was read once; the sed is not a read magi performed, got %d", got)
	}
	if len(o.reread()) != 0 {
		t.Errorf("nothing was opened twice, got %v", o.reread())
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

// `changed: <path>` names each path once — the right shape for "what exists now", the wrong one for
// "how many times you rewrote it". Observed live on large-scale-text-editing: one file authored 14
// times, the same 152 bytes on six of them. The per-call self-edit note said so twenty times, but it
// lives in the tool result, which compaction takes away; this block is what survives.
func TestRewritingOneFileIsCounted(t *testing.T) {
	evs := []event.Event{
		toolCallEv("1", "write", `{"path":"/app/apply_macros.vim","content":"a"}`),
		toolCallEv("2", "write", `{"path":"/app/apply_macros.vim","content":"b"}`),
		toolCallEv("3", "edit", `{"path":"/app/apply_macros.vim"}`),
		toolCallEv("4", "write", `{"path":"/app/other.txt","content":"x"}`),
	}
	o := observeEvents(evs)
	got := o.rewritten()
	if len(got) != 1 || !strings.Contains(got[0], "apply_macros.vim") || !strings.HasSuffix(got[0], "×3") {
		t.Fatalf("only the repeatedly-authored path is worth stating, got %v", got)
	}
	txt := o.render()
	if !strings.Contains(txt, "authored more than once") {
		t.Errorf("the rewrite count must reach the record:\n%s", txt)
	}
	// changed still names each path once — the two lines say different things.
	if strings.Count(txt, "/app/apply_macros.vim") < 2 || !strings.Contains(txt, "/app/other.txt") {
		t.Errorf("changed must still list every path once:\n%s", txt)
	}
}
