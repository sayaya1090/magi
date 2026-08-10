package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"testing"
)

// Every method the screen can call has to be decided about, not defaulted.
//
// `attached` embeds *app.App, so anything it does not override runs against THIS process's engine —
// a throwaway App with no run in it. For a read that is exactly right: the log is shared, and a
// second reader reconstructs the same transcript, which is why only a handful of calls cross the
// socket at all. For a WRITE it is silently wrong, and it was: /model changed a copy nobody was
// using while the daemon kept generating with the old one and the header showed the new name.
// Rewind went further and rewrote the log underneath the process that owns it.
//
// Embedding gives no compiler error for the ones that were missed — that is the whole hazard, and
// it is the same shape as every other defect on this branch: a mechanism whose counterpart does not
// exist. So the decision is written down here, one line per method, and a method added to the
// engine with no entry fails this test rather than quietly picking the wrong process.
func TestEveryEngineMethodIsClassified(t *testing.T) {
	// forwarded: goes to the daemon, because doing it here would change the wrong process.
	forwarded := map[string]bool{
		"Submit": true, "Steer": true, "Interrupt": true,
		"RespondPermission": true, "RespondQuestion": true,
		"Rewind": true, "Compact": true, "SetModel": true, "SetPermission": true,
		"Subscribe": true, // polls the shared log rather than the local bus
	}
	// local: answered from the shared store or this process, and correct there.
	local := map[string]bool{
		"SessionState": true, "ListSessions": true, "UnfinishedTurnOf": true, "Todos": true,
		"ContextView": true, "SessionDiff": true, "GitDiff": true, "Replay": true, "LoopMap": true,
		"UsageFor": true, "UsageTotal": true, "SessionModel": true, "UserLabel": true,
		"CouncilMemberNames": true, "ListModels": true, "ToolNames": true, "Profiles": true,
		"Permission": true, "Subagents": true, "SubagentJobs": true, "CreateSession": true,
		"Fork": true, "RunShell": true, "SetTodos": true,
		// Reading the schedule is reading a file both processes can see.
		"ScheduledJobs": true,
		// Searching past sessions reads the same append-only logs, and reads them the same way.
		"RankSessions": true,
	}
	// hybrid: done HERE, and then the daemon is told.
	//
	// A fourth answer, because three were not enough and the difference is real. These write
	// something both processes can see — a config file on a shared filesystem — so doing it locally
	// is right, and sending the edit over the socket would be asking the daemon to do a thing this
	// process can do perfectly well. What the daemon holds is the DERIVED state it read out of that
	// file, so it has to be told the file moved. Overridden like a forwarded method, and listed
	// apart from them because "goes to the daemon" would be the wrong description to maintain
	// against: it does not.
	hybrid := map[string]string{
		"EditSchedule": "writes the config file here, then calls reload-cron so the running " +
			"daemon re-reads it",
	}
	// known-local-and-wrong: it acts on this process and that is visibly not the daemon's state.
	// Listed rather than fixed, so the gap is a decision on the record instead of a surprise; each
	// needs a protocol call of its own and none of them is dangerous the way Rewind was.
	knownGap := map[string]string{
		"BackgroundJobs":     "the daemon's jobs are in the daemon; this lists none",
		"BackgroundTail":     "same, and there is nothing here to tail",
		"KillBackgroundJob":  "kills nothing; the process holding the job is the daemon",
		"SetProfile":         "changes this process's posture, not the running engine's",
		"SetContextWindow":   "same",
		"SetGroupEnabled":    "toggles a registry the daemon is not using",
		"SetSubagentEnabled": "same",
		"SetSubagentModel":   "same",
	}

	for _, name := range engineMethods(t) {
		switch {
		case forwarded[name], local[name]:
		case knownGap[name] != "", hybrid[name] != "":
		default:
			t.Errorf("engine method %q is not classified: it will run against this process's own "+
				"App, which is right for a read and silently wrong for anything that changes the "+
				"run. Add it to forwarded, local, hybrid or knownGap in this test and do what that "+
				"says.", name)
		}
	}

	// And the ones that are supposed to be overridden must actually be, or the label is a wish.
	overridden := overriddenByAttached(t)
	for name := range forwarded {
		if !overridden[name] {
			t.Errorf("%q is listed as forwarded and `attached` does not override it — it runs "+
				"locally, against an engine with no run in it", name)
		}
	}
	for name := range hybrid {
		if !overridden[name] {
			t.Errorf("%q is listed as hybrid (%s) and `attached` does not override it, so the "+
				"daemon is never told", name, hybrid[name])
		}
	}
	// The reverse: something overridden but not listed is a decision nobody wrote down.
	for name := range overridden {
		if !forwarded[name] && hybrid[name] == "" {
			t.Errorf("`attached` overrides %q, which is in neither the forwarded nor the hybrid set", name)
		}
	}
}

// engineMethods reads the method names off the tui.Engine interface.
func engineMethods(t *testing.T) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "../../internal/adapter/tui/engine.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok || ts.Name.Name != "Engine" {
			return true
		}
		iface, ok := ts.Type.(*ast.InterfaceType)
		if !ok {
			return false
		}
		for _, m := range iface.Methods.List {
			for _, id := range m.Names {
				out = append(out, id.Name)
			}
		}
		return false
	})
	if len(out) < 20 {
		t.Fatalf("read %d methods off tui.Engine; the parser has lost its subject", len(out))
	}
	sort.Strings(out)
	return out
}

// overriddenByAttached finds the methods declared on the attached type.
func overriddenByAttached(t *testing.T) map[string]bool {
	t.Helper()
	src, err := os.ReadFile("attach.go")
	if err != nil {
		t.Fatal(err)
	}
	f, err := parser.ParseFile(token.NewFileSet(), "attach.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || !ast.IsExported(fn.Name.Name) {
			continue
		}
		if id, ok := fn.Recv.List[0].Type.(*ast.Ident); ok && id.Name == "attached" {
			out[fn.Name.Name] = true
		}
	}
	if len(out) < 5 {
		t.Fatalf("found %d overrides on attached; the parser has lost its subject", len(out))
	}
	return out
}
