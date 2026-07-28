package app

import (
	"encoding/json"
	"testing"
)

// Another harness spells a write's destination `file_path` where this one declares `path`. Folding
// case and separators cannot see that (`filepath` is not `path`), so the call ran and died on the
// tool's own "path is required" — and the file it carried was never written. Observed live: a write
// of /app/cleanup_test.py refused this way, and the agent moved on without the file.
//
// The required key being ABSENT is what makes the sent key readable as a rename: the call cannot
// run either way, so naming the real key is strictly better than the tool's own complaint.
func TestQualifiedArgNameIsReadAsTheRequiredKeyItCarries(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}`)
	args := json.RawMessage(`{"file_path":"/app/cleanup_test.py","content":"print(1)"}`)

	misspelled, ignored, declared := unknownToolArgs(schema, args)
	if misspelled["file_path"] != "path" {
		t.Fatalf("`file_path` must be reported as the missing required `path`, got %v (ignored=%v)", misspelled, ignored)
	}
	if len(ignored) != 0 {
		t.Errorf("a key read as a rename must not ALSO be reported as ignored: %v", ignored)
	}
	if len(declared) != 2 {
		t.Errorf("declared keys = %v, want path+content", declared)
	}
}

// The rename reading is bounded by "required AND absent". A key the call legitimately omitted, or
// one it actually sent, must not swallow an unrelated extra — otherwise every `max_lines` on a tool
// with an optional `lines` becomes a refusal of a call that works today.
func TestQualifiedArgNameOnlyClaimsARequiredKeyTheCallOmitted(t *testing.T) {
	// `lines` is optional → `max_lines` stays an ordinary extra.
	optional := json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"lines":{"type":"number"}},"required":["path"]}`)
	if m, ig, _ := unknownToolArgs(optional, json.RawMessage(`{"path":"/a","max_lines":5}`)); len(m) != 0 || len(ig) != 1 || ig[0] != "max_lines" {
		t.Errorf("an optional key must not be claimed as a rename: misspelled=%v ignored=%v", m, ig)
	}
	// `path` required AND sent → `file_path` is an extra, not a rename of a key already present.
	required := json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`)
	if m, ig, _ := unknownToolArgs(required, json.RawMessage(`{"path":"/a","file_path":"/b"}`)); len(m) != 0 || len(ig) != 1 {
		t.Errorf("a required key the call DID send must not be renamed onto: misspelled=%v ignored=%v", m, ig)
	}
	// The component must be whole: `pathological` is not `path`.
	if m, _, _ := unknownToolArgs(required, json.RawMessage(`{"pathological":"/b"}`)); len(m) != 0 {
		t.Errorf("a longer word containing the key is not a rename of it: %v", m)
	}
}

// A source file a language loads by MODULE name is exercised by a command that never contains its
// filename. Observed live (cancel-async-tasks): six `python3 -c "from run import run_tasks …"`
// invocations, all real, and the per-artifact ledger still listed /app/run.py as never run — after
// which the finish nudge told the agent it had never run what it wrote.
func TestAModuleImportCountsAsRunningTheFileItLoads(t *testing.T) {
	cmd := `python3 -c "
import asyncio
from run import run_tasks
asyncio.run(run_tasks([], 2))"`
	if !cmdNamesFile(cmd, "run.py") {
		t.Error("importing a module by its stem must count as naming the file it loads")
	}
	// The plain form still works, and the whole-token rule still holds: `arun` is not `run`.
	if !cmdNamesFile("python3 /app/run.py", "run.py") {
		t.Error("a command naming the file outright must still match")
	}
	if cmdNamesFile("python3 -c 'import arunner'", "run.py") {
		t.Error("a longer identifier containing the stem must not match")
	}
	// Only languages that load by stem: a Go file is named by path, so its stem proves nothing.
	if cmdNamesFile("go test ./cmd", "cmd.go") {
		t.Error("a stem match must not apply to a language that does not import by stem")
	}
	// A two-letter stem is too small to carry meaning in a shell command.
	if cmdNamesFile("cd /app && ls", "ls.py") {
		t.Error("a very short stem must not be matched")
	}
}
