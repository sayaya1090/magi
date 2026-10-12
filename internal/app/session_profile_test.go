package app

import (
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
	"testing"
)

func TestSessionProfileIsolationAndModelChange(t *testing.T) {
	var built ProfileDef
	a := &App{llm: namedLLM{"default"}, bus: bus.New(), states: map[session.SessionID]*sessionState{"one": {meta: session.Session{ID: "one"}}, "two": {meta: session.Session{ID: "two"}}}, cfg: Config{NewProvider: func(p ProfileDef) port.LLMProvider { built = p; return namedLLM{p.BaseURL + "|" + p.APIKey} }}}
	p := ProfileDef{Name: "saved", BaseURL: "https://selected/v1", APIKey: "private-key", Model: "model-a"}
	if err := a.ApplySessionProfile("one", p); err != nil {
		t.Fatal(err)
	}
	if built.BaseURL != p.BaseURL || built.APIKey != p.APIKey || built.Model != p.Model {
		t.Fatal("factory did not receive complete profile")
	}
	if got := unwrapProvider(a.providerFor(a.agentFor(session.Session{ID: "one"}))); got != port.LLMProvider(namedLLM{"https://selected/v1|private-key"}) {
		t.Fatal("wrong selected provider")
	}
	if got := unwrapProvider(a.providerFor(a.agentFor(session.Session{ID: "two"}))); got != port.LLMProvider(namedLLM{"default"}) {
		t.Fatal("other session changed")
	}
	a.SetModel("one", "model-b")
	if a.SessionModel("one") != "model-b" || a.sessionProviderRoute("one") == "" {
		t.Fatal("model change lost route")
	}
	a.states["one"].cancel = func() {}
	if err := a.ApplySessionProfile("one", ProfileDef{BaseURL: "http://other", Model: "other"}); err == nil {
		t.Fatal("active turn accepted")
	}
}
