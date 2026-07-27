package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
)

// encoding/json drops a field the target struct does not declare, without a word. So an argument
// the tool never declared does not fail the call — it vanishes, and the tool answers a DIFFERENT
// question in the ordinary shape of an answer. These cases are the shapes actually recorded across
// the runs (387 of 25291 calls), classified against the REAL registry schemas rather than invented
// ones, because the whole point is which key each tool does and does not declare.
func TestUnknownToolArgsClassifiesRecordedStrays(t *testing.T) {
	reg := builtin.Default()
	schemaOf := func(name string) json.RawMessage {
		tl, ok := reg.Get(name)
		if !ok {
			t.Fatalf("tool %q is not registered", name)
		}
		return tl.Schema()
	}
	cases := []struct {
		tool, args   string
		wantMisspell map[string]string
		wantIgnored  []string
		whyItWasSeen string
	}{{
		// The single largest group, and harmless: bash has no `description`, but nothing is lost by
		// dropping a label. Refusing these would break calls that work today.
		tool: "bash", args: `{"command":"ls","description":"list files"}`,
		wantIgnored:  []string{"description"},
		whyItWasSeen: "another harness shows a per-call label",
	}, {
		// A real loss: the list arrives under a key todowrite does not read, so the tool writes an
		// EMPTY plan — the exact opposite of the call's intent, reported as success.
		tool: "todowrite", args: `{".todos":[{"content":"a","status":"pending"}]}`,
		wantMisspell: map[string]string{".todos": "todos"},
		whyItWasSeen: "a stray leading dot in the emitted key",
	}, {
		tool: "todowrite", args: `{"Todos":[]}`,
		wantMisspell: map[string]string{"Todos": "todos"},
		whyItWasSeen: "capitalization",
	}, {
		// A silently DIFFERENT search: the flag is dropped, so the answer is case-sensitive and
		// looks like a fact about the tree.
		tool: "grep", args: `{"pattern":"foo","ignore_case":true}`,
		wantIgnored:  []string{"ignore_case"},
		whyItWasSeen: "a shell flag written as a JSON key",
	}, {
		tool: "grep", args: `{"pattern":"foo","-i":true,"-n":true}`,
		wantIgnored:  []string{"-i", "-n"},
		whyItWasSeen: "shell flags verbatim",
	}, {
		// write declares path/content; another harness's names match neither, so the write runs with
		// no content at all.
		tool: "write", args: `{"file_path":"/a","file_text":"x"}`,
		wantIgnored:  []string{"file_path", "file_text"},
		whyItWasSeen: "another harness's argument names",
	}, {
		tool: "write", args: `{"Path":"/a","content":"x"}`,
		wantMisspell: map[string]string{"Path": "path"},
		whyItWasSeen: "capitalization",
	}, {
		// The overwhelming majority: nothing to report at all.
		tool: "grep", args: `{"pattern":"foo","path":"src","glob":"*.go"}`,
		whyItWasSeen: "a correct call",
	}}
	for _, c := range cases {
		mis, ign, decl := unknownToolArgs(schemaOf(c.tool), json.RawMessage(c.args))
		if len(mis) != len(c.wantMisspell) {
			t.Errorf("%s %s (%s): misspelled = %v, want %v", c.tool, c.args, c.whyItWasSeen, mis, c.wantMisspell)
		}
		for k, want := range c.wantMisspell {
			if mis[k] != want {
				t.Errorf("%s %s: misspelled[%q] = %q, want %q", c.tool, c.args, k, mis[k], want)
			}
		}
		if strings.Join(ign, ",") != strings.Join(c.wantIgnored, ",") {
			t.Errorf("%s %s (%s): ignored = %v, want %v", c.tool, c.args, c.whyItWasSeen, ign, c.wantIgnored)
		}
		if len(decl) == 0 {
			t.Errorf("%s: declared list is empty — the message could not name the real arguments", c.tool)
		}
	}
}

// The check must never invent a complaint about a call it could not read: a schema with no
// properties, a non-object argument payload, and malformed JSON all mean "no opinion".
func TestUnknownToolArgsStaysSilentWhenItCannotJudge(t *testing.T) {
	cases := []struct{ name, schema, args string }{
		{"schema declares nothing", `{"type":"object"}`, `{"anything":1}`},
		{"schema is not JSON", `not json`, `{"anything":1}`},
		{"args are not an object", `{"properties":{"a":{}}}`, `["a"]`},
		{"args are malformed", `{"properties":{"a":{}}}`, `{`},
		{"args are empty", `{"properties":{"a":{}}}`, `{}`},
	}
	for _, c := range cases {
		mis, ign, _ := unknownToolArgs(json.RawMessage(c.schema), json.RawMessage(c.args))
		if len(mis) != 0 || len(ign) != 0 {
			t.Errorf("%s: got misspelled=%v ignored=%v, want nothing", c.name, mis, ign)
		}
	}
}

// The refusal has to carry the correction, otherwise it is the same dead end as a bare "unknown
// tool" reply: the model re-issues the identical call.
func TestRenamesForNamesTheRealKey(t *testing.T) {
	got := renamesFor(map[string]string{".todos": "todos", "Path": "path"})
	for _, want := range []string{"`.todos` is `todos`", "`Path` is `path`"} {
		if !strings.Contains(got, want) {
			t.Errorf("renamesFor() = %q, missing %q", got, want)
		}
	}
}

func TestNormArgKeyFoldsCaseAndSeparators(t *testing.T) {
	cases := []struct{ in, want string }{
		{".todos", "todos"}, {"Todos", "todos"}, {"todo_s", "todos"}, {" todos ", "todos"},
		{"todo-list", "todolist"}, {"-i", "i"}, {"old_string", "oldstring"},
	}
	for _, c := range cases {
		if got := normArgKey(c.in); got != c.want {
			t.Errorf("normArgKey(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
