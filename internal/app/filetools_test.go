package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// declaredTool is what an editor plugin's edit tool looks like from here: a name none of the
// built-in lists contain, and a path argument that is not called "path".
type declaredTool struct {
	name   string
	writes bool
}

func (d declaredTool) Name() string            { return d.name }
func (d declaredTool) Description() string     { return "edits a file through the IDE" }
func (d declaredTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (d declaredTool) WritesFile() bool        { return d.writes }
func (d declaredTool) FileArg(a json.RawMessage) string {
	var v struct {
		File string `json:"file"`
	}
	_ = json.Unmarshal(a, &v)
	return v.File
}
func (d declaredTool) Execute(context.Context, json.RawMessage, port.ToolEnv) (session.ToolResult, error) {
	return session.ToolResult{}, nil
}

type oneTool struct{ t port.Tool }

func (r oneTool) Register(port.Tool) {}
func (r oneTool) Unregister(string)  {}
func (r oneTool) List() []port.Tool  { return []port.Tool{r.t} }
func (r oneTool) Get(n string) (port.Tool, bool) {
	if r.t != nil && r.t.Name() == n {
		return r.t, true
	}
	return nil, false
}

// The five things that hang off "this was an edit" used to hang off three names. A tool that edits
// the same workspace under any other name got none of them — which is every tool an editor plugin
// or a slide add-in attaches, because those are called mcp__<server>__<tool>.
func TestAToolThatSaysItEditsIsTreatedAsAnEdit(t *testing.T) {
	a := &App{tools: oneTool{declaredTool{name: "mcp__jetbrains__edit", writes: true}}}

	if !a.changesFile("mcp__jetbrains__edit") {
		t.Error("a tool that declares it writes files was not read as an edit")
	}
	got := a.filePathOf("mcp__jetbrains__edit", json.RawMessage(`{"file":"/w/main.go"}`))
	if got != "/w/main.go" {
		t.Errorf("path %q — the tool says where its path is; magi used to assume an argument called \"path\"", got)
	}
	// And the builtin names still answer, for a registry that holds none of this.
	if !a.changesFile("edit") || a.changesFile("read") || a.changesFile("bash") {
		t.Error("the builtin vocabulary changed")
	}
}

// A tool that declares it only READS is not an edit. Otherwise every declaring tool would bump the
// guard's epoch and count as progress, which is the opposite of what the declaration is for.
func TestAReadingToolIsNotAnEdit(t *testing.T) {
	a := &App{tools: oneTool{declaredTool{name: "mcp__jetbrains__open", writes: false}}}
	if a.changesFile("mcp__jetbrains__open") {
		t.Error("a declared reader was counted as a file change")
	}
	if got := a.filePathOf("mcp__jetbrains__open", json.RawMessage(`{"file":"/w/x.go"}`)); got != "/w/x.go" {
		t.Errorf("a reader's path is still its path: %q", got)
	}
}

// The floor is the half that cannot move to the result: it has to answer before the write. A
// declared file tool is refused a secret exactly as the builtins are — and the refusal is the hard
// kind, not the confirmation prompt an unknown tool would get.
func TestTheSecretFloorHoldsForADeclaredTool(t *testing.T) {
	p := newPolicy(nil, nil, nil)
	a := &App{tools: oneTool{declaredTool{name: "mcp__jetbrains__edit", writes: true}}}
	p.touches = a.touchesFile

	for _, path := range []string{"/w/.env", "/w/deploy/id_rsa"} {
		args, _ := json.Marshal(map[string]string{"file": path})
		verdict, reason := p.Decide("mcp__jetbrains__edit", args)
		if verdict != "deny" {
			t.Errorf("%s through a declared editor: verdict=%q reason=%q — the floor is a floor or it is not",
				path, verdict, reason)
		}
	}

	// The guardrail half is writes-only: an agent may read its own config and may not rewrite it.
	args, _ := json.Marshal(map[string]string{"file": "/w/.magi/config.toml"})
	if v, _ := p.Decide("mcp__jetbrains__edit", args); v != "deny" {
		t.Errorf("a declared editor rewrote the guardrail file: %q", v)
	}
	reader := &App{tools: oneTool{declaredTool{name: "mcp__jetbrains__open", writes: false}}}
	p2 := newPolicy(nil, nil, nil)
	p2.touches = reader.touchesFile
	if v, _ := p2.Decide("mcp__jetbrains__open", args); v == "deny" {
		t.Error("reading .magi/config.toml was denied — the guardrail is about writing it")
	}
	// …but a secret is refused to a reader too.
	secret, _ := json.Marshal(map[string]string{"file": "/w/.env"})
	if v, _ := p2.Decide("mcp__jetbrains__open", secret); v != "deny" {
		t.Errorf("a declared reader was allowed a secret: %q", v)
	}
}

// A tool that declares nothing is unchanged: no path, no floor, no edit machinery. The declaration
// adds a way in; it does not put every MCP tool through the file path.
func TestAToolThatDeclaresNothingIsUntouched(t *testing.T) {
	a := &App{tools: oneTool{nil}}
	if _, ok := a.touchesFile("mcp__ppt__render", json.RawMessage(`{"path":"/w/.env"}`)); ok {
		t.Error("an undeclared tool was treated as a file tool because its argument was called path")
	}
	p := newPolicy(nil, nil, nil)
	p.touches = a.touchesFile
	if v, _ := p.Decide("mcp__ppt__render", json.RawMessage(`{"path":"/w/.env"}`)); v == "deny" {
		t.Error("the floor fired for a tool that never said it opens files — it goes through the danger gate instead")
	}
}

// The evidence the council reads is "what changed this turn", and it was gathered by the same three
// names. A tool that edits under any other one left no trace in it — so a turn that did its work
// through an editor plugin read as a turn that changed nothing.
func TestTheCouncilSeesADeclaredToolsEdit(t *testing.T) {
	a := &App{tools: oneTool{declaredTool{name: "mcp__jetbrains__edit", writes: true}}}
	evs := []event.Event{toolCallEv("c1", "mcp__jetbrains__edit", `{"file":"main.go"}`)}

	if got := observeEvents(evs, a.touchesFile).changed; len(got) != 1 || got[0] != "main.go" {
		t.Errorf("the council was shown %v — a turn that edited a file read as one that changed nothing", got)
	}
	// And with no answer behind it, the builtin names still are the vocabulary.
	if got := observeEvents(evs, nil).changed; len(got) != 0 {
		t.Errorf("an undeclared name counted as a change: %v", got)
	}
}

// "This run created it" decides whether an irreversible command is asked about, so the question has
// to be asked of the filesystem. It was asked of the CONTENT: an empty before-snapshot meant a
// creation — and a before-snapshot is also empty when the file was too large to snapshot at all
// (readForChange stops at its cap, because part of a file is not the file).
//
// So every write to a file over that cap was recorded as a creation, and a run could then delete a
// file it had only edited without the gate ever asking. The bash path has always taken the stat,
// with the reason written beside it; this is the same question, answered the same way.
func TestALargeFileThatWasEditedIsNotRecordedAsCreated(t *testing.T) {
	dir := t.TempDir()
	big := filepath.Join(dir, "deck.bin")
	if err := os.WriteFile(big, make([]byte, changeReadCap+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if before, ok := readForChange(dir, big); before != "" || ok {
		t.Fatalf("the premise moved: a file over the cap now snapshots as %q/%v", before, ok)
	}

	a := &App{tools: oneTool{declaredTool{name: "write", writes: true}}}
	guard := newRunGuard(a.touchesFile)
	args, _ := json.Marshal(map[string]string{"file": big})
	a.noteToolOutcome("s1", guard, toolOutcome{
		tc: &session.ToolCall{CallID: "c1", Name: "write", Args: args}, res: &session.ToolResult{},
		workdir: dir, fp: "fp", toolOK: true,
		changePath: big, changeBefore: "", changeExisted: true, // it WAS there; it was only edited
	})
	if guard.didCreate(big) {
		t.Error("editing a file the run did not create was recorded as creating it — " +
			"the gate on irreversible commands reads this, and would let the run delete it unasked")
	}

	// …and a file this run really did make is still recorded, or the gate asks about the run's own
	// scratch output forever.
	made := filepath.Join(dir, "new.txt")
	if err := os.WriteFile(made, []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}
	args2, _ := json.Marshal(map[string]string{"file": made})
	a.noteToolOutcome("s1", guard, toolOutcome{
		tc: &session.ToolCall{CallID: "c2", Name: "write", Args: args2}, res: &session.ToolResult{},
		workdir: dir, fp: "fp2", toolOK: true,
		changePath: made, changeBefore: "", changeExisted: false,
	})
	if !guard.didCreate(made) {
		t.Error("a file this run wrote from nothing was not recorded as created")
	}
}

// Declaring adds obligations; it must never subtract a question.
//
// Under --permission auto ("accept edits") magi's own file tools are approved without asking,
// because they are confined to the workspace. When the recogniser stopped being a list of three
// names, that shortcut silently widened to any tool that declared itself — including an MCP tool,
// which is another process with this machine's privileges and no jail, and which has always been
// asked about through the danger gate. The first thing a tool would have learned is that saying "I
// edit files" is how you stop being asked.
func TestDeclaringDoesNotBuyAnMCPToolAutoApproval(t *testing.T) {
	a := &App{tools: oneTool{declaredTool{name: "mcp__jetbrains__edit", writes: true}}}
	if !a.changesFile("mcp__jetbrains__edit") {
		t.Fatal("the premise moved: a declared editor is no longer read as an edit")
	}
	if confinedEdit("mcp__jetbrains__edit") {
		t.Error("a declared MCP editor would run unasked under --permission auto")
	}
	// …and magi's own edit tools still do, or accept-edits stops meaning anything.
	for _, own := range []string{"write", "edit", "multiedit"} {
		if !confinedEdit(own) {
			t.Errorf("%s lost its accept-edits shortcut", own)
		}
	}
	// A reader is not an edit either, whoever declares it.
	if confinedEdit("read") {
		t.Error("reading a file counted as an edit")
	}
}
