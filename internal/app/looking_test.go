package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// A session opened to answer a question may only look, and nothing else in the workspace has to
// wait for it.
//
// Two halves of one fact, which is why they are one test: the role decides the tools, and the same
// role is what lets WritingRun say the workspace is free. Wire only the first and a question runs
// read-only while everything still queues behind it; wire only the second and handed work starts
// beside a turn that can write, which is the collision the queue exists to prevent.
func TestASessionThatOnlyLooksHasReadOnlyToolsAndBlocksNobody(t *testing.T) {
	a := &App{cfg: Config{}}
	a.cfg = a.cfg.withDefaults()

	spec := a.agentFor(session.Session{ID: "s1", Agent: LookingAgent})
	if len(spec.Tools) == 0 {
		t.Fatal("a session that may only look was given the whole tool set")
	}
	for _, n := range spec.Tools {
		if !readOnlyTools[n] {
			t.Errorf("%q is in the allowlist of a session that may only look", n)
		}
	}
	// …and an ordinary one is untouched: its tools are its agent's, narrowed elsewhere.
	if got := a.agentFor(session.Session{ID: "s2"}); len(got.Tools) != 0 {
		t.Errorf("an ordinary session came back with an allowlist: %v", got.Tools)
	}
}

// The gate that handed-over work asks: is anything running that could touch the workspace?
func TestWritingRunIgnoresATurnThatCannotWrite(t *testing.T) {
	a := &App{cfg: Config{}}
	a.cfg = a.cfg.withDefaults()

	// A session that may only look, with a turn in flight.
	looking := session.SessionID("s_looking")
	a.mu.Lock()
	a.stateLocked(looking).meta = session.Session{ID: looking, Agent: LookingAgent}
	a.stateLocked(looking).cancel = func() {}
	a.mu.Unlock()

	if sid, busy := a.Running(); !busy {
		t.Errorf("Running says nothing is in flight while %q is: it answers about the PROCESS", sid)
	}
	if sid, busy := a.WritingRun(); busy {
		t.Errorf("a turn that cannot write is reported as holding the workspace: %q", sid)
	}

	// And an ordinary one does hold it.
	ordinary := session.SessionID("s_work")
	a.mu.Lock()
	a.stateLocked(ordinary).meta = session.Session{ID: ordinary}
	a.stateLocked(ordinary).cancel = func() {}
	a.mu.Unlock()
	if _, busy := a.WritingRun(); !busy {
		t.Error("a turn that can write is not reported as holding the workspace")
	}
}

// declaresReadOnly is an MCP-backed tool the way the office helper's read tools arrive: a name no
// built-in list contains, and its server's own word that it changes nothing.
type declaresReadOnly struct {
	name string
	ro   bool
}

func (d declaresReadOnly) Name() string            { return d.name }
func (d declaresReadOnly) Description() string     { return "reads a document through the add-in" }
func (d declaresReadOnly) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (d declaresReadOnly) ReadOnly() bool          { return d.ro }
func (d declaresReadOnly) Execute(context.Context, json.RawMessage, port.ToolEnv) (session.ToolResult, error) {
	return session.ToolResult{}, nil
}

// A looking session may use a tool that DECLARES it only reads, even though its name is not one of
// the four.
//
// Measured (2026-09-07): a looking hand_off to the Word companion answered that it had no tool to
// read the document with. The four names are filesystem tools, and a companion whose workspace is a
// document has nothing to read with them — so a read-only question about the document could not be
// answered at all. The writing tool beside it stays out: the role still means what it says.
func TestALookingSessionMayUseToolsThatDeclareTheyOnlyRead(t *testing.T) {
	reads := declaresReadOnly{name: "mcp__word__list_paragraphs", ro: true}
	writes := declaresReadOnly{name: "mcp__word__insert_paragraph", ro: false}
	a := &App{cfg: Config{}, tools: manyTools{reads, writes}}
	a.cfg = a.cfg.withDefaults()

	looking := a.agentFor(session.Session{ID: "s1", Agent: LookingAgent})
	got := map[string]bool{}
	for _, s := range a.toolSpecs("s1", looking) {
		got[s.Name] = true
	}
	if !got[reads.name] {
		t.Error("a tool that says it only reads was kept from a session that may only look")
	}
	if got[writes.name] {
		t.Error("a tool that does NOT say it only reads was offered to a session that may only look")
	}

	// An ordinary session is untouched — it had both already.
	ordinary := a.agentFor(session.Session{ID: "s2"})
	if n := len(a.toolSpecs("s2", ordinary)); n != 2 {
		t.Errorf("an ordinary session saw %d tools, not both", n)
	}
}

// manyTools is a registry over a fixed set.
type manyTools []port.Tool

func (m manyTools) Register(port.Tool) {}
func (m manyTools) Unregister(string)  {}
func (m manyTools) List() []port.Tool  { return m }
func (m manyTools) Get(n string) (port.Tool, bool) {
	for _, t := range m {
		if t.Name() == n {
			return t, true
		}
	}
	return nil, false
}

// **광고와 실행이 같은 문을 지난다.** 앞 판은 목록만 넓혀서, 모델이 목록에서 본 도구를 부르면 디스패치가
// "tool not permitted for agent looking" 으로 거절했다(실측 2026-09-07, 워드 컴패니언). 두 자리가 갈리면
// 모델은 있는 도구를 부르고 없다는 말을 듣는다 — 재시도 말고는 할 것이 없는 막다른 길이다.
func TestWhatALookingSessionIsOfferedIsAlsoWhatItMayCall(t *testing.T) {
	reads := declaresReadOnly{name: "mcp__word__list_paragraphs", ro: true}
	writes := declaresReadOnly{name: "mcp__word__insert_paragraph", ro: false}
	a := &App{cfg: Config{}, tools: manyTools{reads, writes}}
	a.cfg = a.cfg.withDefaults()
	looking := a.agentFor(session.Session{ID: "s1", Agent: LookingAgent})

	for _, spec := range a.toolSpecs("s1", looking) {
		tool, ok := a.tools.Get(spec.Name)
		if !ok {
			t.Fatalf("%s 를 광고했는데 등록부에 없다", spec.Name)
		}
		if !looking.allows(spec.Name) && !looksOnlyReads(looking, tool) {
			t.Errorf("%s 를 광고해 놓고 부르면 거절한다", spec.Name)
		}
	}
	// 쓰기 도구는 여전히 못 부른다 — 넓힌 것은 「읽기만 한다고 선언한」 것뿐이다.
	if w, _ := a.tools.Get(writes.name); looking.allows(writes.name) || looksOnlyReads(looking, w) {
		t.Error("읽기 전용 턴이 쓰기 도구를 부를 수 있다")
	}
}
